package progressutil

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"aegis/pkg/timeutil"
	"github.com/schollz/progressbar/v3"
	"golang.org/x/term"
)

type Progress struct {
	mu  sync.Mutex
	bar *progressbar.ProgressBar
}

func New(total int64, description string) *Progress {
	if total < 0 {
		total = 0
	}
	writer := progressWriter()
	bar := progressbar.NewOptions64(total,
		progressbar.OptionSetWriter(writer),
		progressbar.OptionSetDescription(strings.TrimSpace(description)),
		progressbar.OptionSetWidth(24),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionSetElapsedTime(true),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString("step"),
		progressbar.OptionThrottle(timeutil.Milliseconds(120)),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionUseANSICodes(term.IsTerminal(int(os.Stdout.Fd()))),
	)
	return &Progress{bar: bar}
}

func (p *Progress) Add(delta int64) {
	if p == nil || p.bar == nil || delta == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.bar.Add64(delta)
}

func (p *Progress) SetDescription(format string, args ...any) {
	if p == nil || p.bar == nil {
		return
	}
	description := strings.TrimSpace(fmt.Sprintf(format, args...))
	p.mu.Lock()
	defer p.mu.Unlock()
	p.bar.Describe(description)
}

func (p *Progress) Finish() {
	if p == nil || p.bar == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.bar.Finish()
}

func progressWriter() io.Writer {
	if term.IsTerminal(int(os.Stdout.Fd())) {
		return os.Stdout
	}
	return io.Discard
}
