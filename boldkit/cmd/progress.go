package cmd

import (
	"os"
	"time"

	boldio "github.com/Doomsbay/BoldKit/internal/io"
	"github.com/schollz/progressbar/v3"
)

type progress struct {
	bar *progressbar.ProgressBar
}

func newProgress(total, reportEvery int) *progress {
	if reportEvery == 0 {
		return &progress{bar: nil}
	}

	opts := []progressbar.Option{
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionThrottle(250 * time.Millisecond),
		progressbar.OptionClearOnFinish(),
	}

	var bar *progressbar.ProgressBar
	if total > 0 {
		opts = append(opts,
			progressbar.OptionSetWidth(30),
			progressbar.OptionShowCount(),
			progressbar.OptionShowIts(),
			progressbar.OptionSetPredictTime(true),
		)
		bar = progressbar.NewOptions(total, opts...)
	} else {
		opts = append(opts,
			progressbar.OptionSpinnerType(14),
			progressbar.OptionShowCount(),
			progressbar.OptionShowIts(),
		)
		bar = progressbar.NewOptions(-1, opts...)
	}

	return &progress{bar: bar}
}

func (p *progress) Increment() {
	if p.bar == nil {
		return
	}
	_ = p.bar.Add(1)
}

func (p *progress) Finish() {
	if p.bar == nil {
		return
	}
	_ = p.bar.Finish()
}

type byteProgress struct {
	bar *progressbar.ProgressBar
}

func newByteProgress(total int64, label string) *byteProgress {
	opts := []progressbar.Option{
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionThrottle(250 * time.Millisecond),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionShowBytes(true),
	}
	if label != "" {
		opts = append(opts, progressbar.OptionSetDescription(label))
	}

	if total > 0 {
		opts = append(opts,
			progressbar.OptionSetWidth(30),
			progressbar.OptionShowCount(),
			progressbar.OptionShowIts(),
			progressbar.OptionSetPredictTime(true),
		)
		return &byteProgress{bar: progressbar.NewOptions64(total, opts...)}
	}
	opts = append(opts,
		progressbar.OptionSpinnerType(14),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
	)
	return &byteProgress{bar: progressbar.NewOptions(-1, opts...)}
}

func (b *byteProgress) Add(delta int64) {
	if b == nil || b.bar == nil {
		return
	}
	_ = b.bar.Add64(delta)
}

func (b *byteProgress) Finish() {
	if b == nil || b.bar == nil {
		return
	}
	_ = b.bar.Finish()
}

func updateByteProgress(bar *byteProgress, counter *boldio.CountReader, last *int64) {
	if bar == nil || counter == nil || last == nil {
		return
	}
	cur := counter.Count()
	delta := cur - *last
	if delta <= 0 {
		return
	}
	bar.Add(delta)
	*last = cur
}
