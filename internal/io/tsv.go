package io

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type Progress interface {
	Increment()
	Finish()
}

const (
	defaultBufferSize = 1 << 20
	defaultChunkSize  = 8 << 20
	defaultBatchLines = 1024
)

type Options struct {
	BufferSize           int
	ChunkSize            int
	BatchLines           int
	Workers              int
	StrictColumns        bool
	ExpectedColumns      int
	PreserveOrder        bool
	AllowCRLF            bool
	Progress             Progress
	SkipProgressFirstRow bool
	Timeout              time.Duration
}

type Row struct {
	Line   int64
	Fields [][]byte
}

type bufferRef struct {
	buf  []byte
	pool *sync.Pool
	slot *pooledBuf
	ref  int32
}

type pooledBuf struct {
	buf []byte
}

func (b *bufferRef) release() {
	if b == nil {
		return
	}
	if atomic.AddInt32(&b.ref, -1) == 0 {
		if b.slot != nil {
			b.slot.buf = b.buf[:cap(b.buf)]
			b.pool.Put(b.slot)
		}
	}
}

type lineBatch struct {
	seq      int64
	buf      *bufferRef
	lines    [][]byte
	lineNums []int64
}

type parseResult struct {
	seq  int64
	rows []Row
	err  error
	buf  *bufferRef
}

func DefaultOptions() Options {
	return Options{
		BufferSize:    defaultBufferSize,
		ChunkSize:     defaultChunkSize,
		BatchLines:    defaultBatchLines,
		Workers:       runtime.GOMAXPROCS(0),
		PreserveOrder: true,
		AllowCRLF:     true,
	}
}

func (o Options) WithPreserveOrder(preserve bool) Options {
	o.PreserveOrder = preserve
	return o
}

func (o Options) WithAllowCRLF(allow bool) Options {
	o.AllowCRLF = allow
	return o
}

func (o Options) withDefaults() Options {
	if o.BufferSize <= 0 {
		o.BufferSize = defaultBufferSize
	}
	if o.ChunkSize <= 0 {
		o.ChunkSize = defaultChunkSize
	}
	if o.BatchLines <= 0 {
		o.BatchLines = defaultBatchLines
	}
	if o.Workers <= 0 {
		o.Workers = runtime.GOMAXPROCS(0)
	}
	return o
}

func ParseTSV(r io.Reader, opts Options, onRow func(Row) error) error {
	opts = opts.withDefaults()

	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
	if opts.Timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), opts.Timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	bufPool := &sync.Pool{
		New: func() any {
			return &pooledBuf{buf: make([]byte, opts.ChunkSize)}
		},
	}

	batches := make(chan *lineBatch, opts.Workers*2)
	results := make(chan parseResult, opts.Workers*2)
	readErrCh := make(chan error, 1)

	go func() {
		reader := bufio.NewReaderSize(r, opts.BufferSize)
		readErrCh <- readBatches(ctx, reader, opts, bufPool, batches)
		close(batches)
	}()

	var workerWG sync.WaitGroup
	for i := 0; i < opts.Workers; i++ {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			workerLoop(opts, batches, results)
		}()
	}

	go func() {
		workerWG.Wait()
		close(results)
	}()

	err := consumeResults(ctx, opts, results, cancel, onRow)
	if err != nil {
		cancel()
	}

	readErr := <-readErrCh
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if readErr != nil && readErr != context.Canceled {
		return readErr
	}
	return nil
}

func ParseTSVChan(ctx context.Context, r io.Reader, opts Options) (<-chan Row, <-chan error) {
	opts = opts.withDefaults()

	rowsCh := make(chan Row, opts.BatchLines)
	errCh := make(chan error, 1)

	go func() {
		defer close(rowsCh)
		errCh <- ParseTSV(r, opts, func(row Row) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case rowsCh <- copyRow(row):
				return nil
			}
		})
		close(errCh)
	}()

	return rowsCh, errCh
}

func copyRow(row Row) Row {
	copied := Row{
		Line:   row.Line,
		Fields: make([][]byte, len(row.Fields)),
	}
	for i, f := range row.Fields {
		dst := make([]byte, len(f))
		copy(dst, f)
		copied.Fields[i] = dst
	}
	return copied
}

func readBatches(ctx context.Context, r *bufio.Reader, opts Options, pool *sync.Pool, batches chan<- *lineBatch) error {
	tail := make([]byte, 0, 1024)
	var seq int64
	var lineNum int64

	for {
		if ctx.Err() != nil {
			return context.Canceled
		}

		slot := pool.Get().(*pooledBuf)
		buf := slot.buf
		needed := opts.ChunkSize + len(tail)
		if cap(buf) < needed {
			buf = make([]byte, needed)
		}

		copy(buf, tail)
		n, err := r.Read(buf[len(tail):needed])
		if n == 0 && err == io.EOF {
			slot.buf = buf[:cap(buf)]
			pool.Put(slot)
			break
		}
		if err != nil && err != io.EOF && n == 0 {
			slot.buf = buf[:cap(buf)]
			pool.Put(slot)
			return err
		}

		dataLen := len(tail) + n
		data := buf[:dataLen]
		lines := make([][]byte, 0, opts.BatchLines*2)
		lineNums := make([]int64, 0, opts.BatchLines*2)

		start := 0
		for i, b := range data {
			if b == '\n' {
				line := data[start:i]
				if opts.AllowCRLF && len(line) > 0 && line[len(line)-1] == '\r' {
					line = line[:len(line)-1]
				}
				lineNum++
				lines = append(lines, line)
				lineNums = append(lineNums, lineNum)
				start = i + 1
			}
		}

		tail = tail[:0]
		if start < len(data) {
			tail = append(tail, data[start:]...)
		}

		if len(lines) > 0 {
			batchSize := opts.BatchLines
			if batchSize > len(lines) {
				batchSize = len(lines)
			}
			batchCount := (len(lines) + batchSize - 1) / batchSize
			ref := &bufferRef{
				buf:  buf[:dataLen],
				pool: pool,
				slot: slot,
				ref:  int32(batchCount),
			}

			for i := 0; i < batchCount; i++ {
				startIdx := i * batchSize
				endIdx := startIdx + batchSize
				if endIdx > len(lines) {
					endIdx = len(lines)
				}

				batch := &lineBatch{
					seq:      seq,
					buf:      ref,
					lines:    lines[startIdx:endIdx],
					lineNums: lineNums[startIdx:endIdx],
				}
				seq++

				select {
				case batches <- batch:
				case <-ctx.Done():
					ref.release()
					return context.Canceled
				}
			}
		} else {
			slot.buf = buf[:cap(buf)]
			pool.Put(slot)
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	if len(tail) > 0 {
		slot := pool.Get().(*pooledBuf)
		buf := slot.buf
		if cap(buf) < len(tail) {
			buf = make([]byte, len(tail))
		}
		copy(buf, tail)
		ref := &bufferRef{
			buf:  buf[:len(tail)],
			pool: pool,
			slot: slot,
			ref:  1,
		}
		lineNum++
		batch := &lineBatch{
			seq:      seq,
			buf:      ref,
			lines:    [][]byte{ref.buf},
			lineNums: []int64{lineNum},
		}
		select {
		case batches <- batch:
		case <-ctx.Done():
			ref.release()
			return context.Canceled
		}
	}

	return nil
}

func workerLoop(opts Options, batches <-chan *lineBatch, results chan<- parseResult) {
	for batch := range batches {
		rows := make([]Row, 0, len(batch.lines))
		for i, line := range batch.lines {
			fields := splitFields(line, opts.ExpectedColumns)
			rows = append(rows, Row{
				Line:   batch.lineNums[i],
				Fields: fields,
			})
		}
		results <- parseResult{
			seq:  batch.seq,
			rows: rows,
			buf:  batch.buf,
		}
	}
}

func consumeResults(ctx context.Context, opts Options, results <-chan parseResult, cancel context.CancelFunc, onRow func(Row) error) error {
	expectedSeq := int64(0)
	pending := make(map[int64]parseResult)
	var err error
	expectedColumns := opts.ExpectedColumns
	var rowsSeen int64

	processResult := func(res parseResult) {
		if res.err != nil && err == nil {
			err = res.err
			cancel()
		}
		if err != nil {
			res.buf.release()
			return
		}

		for _, row := range res.rows {
			if ctx.Err() != nil {
				err = ctx.Err()
				break
			}
			if opts.StrictColumns {
				if expectedColumns == 0 {
					expectedColumns = len(row.Fields)
				} else if len(row.Fields) != expectedColumns {
					err = fmt.Errorf("line %d: expected %d columns, got %d", row.Line, expectedColumns, len(row.Fields))
					break
				}
			}
			if opts.Progress != nil {
				if !opts.SkipProgressFirstRow || rowsSeen != 0 {
					opts.Progress.Increment()
				}
			}
			rowsSeen++
			if cbErr := onRow(row); cbErr != nil {
				err = cbErr
				break
			}
		}
		res.buf.release()
		if err != nil {
			cancel()
		}
	}

	if opts.PreserveOrder {
		for res := range results {
			if err != nil {
				res.buf.release()
				continue
			}

			pending[res.seq] = res
			for {
				next, ok := pending[expectedSeq]
				if !ok {
					break
				}
				delete(pending, expectedSeq)
				processResult(next)
				expectedSeq++
				if err != nil {
					break
				}
			}
		}

		if err != nil {
			for _, res := range pending {
				res.buf.release()
			}
		} else if len(pending) > 0 {
			for _, res := range pending {
				processResult(res)
			}
		}
	} else {
		for res := range results {
			if err != nil {
				res.buf.release()
				continue
			}
			processResult(res)
		}
	}

	return err
}

func splitFields(line []byte, expected int) [][]byte {
	capacity := expected
	if capacity == 0 {
		capacity = 8
	}
	fields := make([][]byte, 0, capacity)

	start := 0
	for i, b := range line {
		if b == '\t' {
			fields = append(fields, line[start:i])
			start = i + 1
		}
	}
	fields = append(fields, line[start:])
	return fields
}
