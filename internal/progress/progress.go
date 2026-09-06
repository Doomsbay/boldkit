package progress

import (
	"os"
	"time"

	"github.com/schollz/progressbar/v3"
)

type Progress struct {
	bar *progressbar.ProgressBar
}

func New(total, reportEvery int) *Progress {
	if reportEvery == 0 {
		return &Progress{bar: nil}
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

	return &Progress{bar: bar}
}

func (p *Progress) Increment() {
	if p.bar == nil {
		return
	}
	_ = p.bar.Add(1)
}

func (p *Progress) Finish() {
	if p.bar == nil {
		return
	}
	_ = p.bar.Finish()
}

type ByteProgress struct {
	bar *progressbar.ProgressBar
}

func NewByte(total int64, label string) *ByteProgress {
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
		return &ByteProgress{bar: progressbar.NewOptions64(total, opts...)}
	}
	opts = append(opts,
		progressbar.OptionSpinnerType(14),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
	)
	return &ByteProgress{bar: progressbar.NewOptions(-1, opts...)}
}

func (b *ByteProgress) Add(delta int64) {
	if b == nil || b.bar == nil {
		return
	}
	_ = b.bar.Add64(delta)
}

func (b *ByteProgress) Finish() {
	if b == nil || b.bar == nil {
		return
	}
	_ = b.bar.Finish()
}
