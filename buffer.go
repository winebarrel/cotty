package cotty

import (
	"bytes"
	"context"
	"regexp"
	"sync"
	"time"
	"unicode/utf8"
)

// outputBuffer keeps the most recent output of a session as plain text.
//
// Positions are absolute byte offsets from the start of the session, so a
// reader can carry one between calls and pick up where it left off even after
// older output has been discarded.
type outputBuffer struct {
	mu        sync.Mutex
	data      []byte
	start     int64
	limit     int
	lastWrite time.Time
	closed    bool
	changed   chan struct{}
}

func newOutputBuffer(limit int) *outputBuffer {
	return &outputBuffer{
		limit:   limit,
		changed: make(chan struct{}),
	}
}

func (b *outputBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.data = append(b.data, p...)

	if over := len(b.data) - b.limit; over > 0 {
		b.data = b.data[over:]
		b.start += int64(over)
	}

	b.lastWrite = time.Now()

	if !b.closed {
		close(b.changed)
		b.changed = make(chan struct{})
	}

	return len(p), nil
}

// Close marks the end of output and wakes every waiting reader.
func (b *outputBuffer) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.closed {
		b.closed = true
		close(b.changed)
	}
}

// End returns the offset just past the last byte written.
func (b *outputBuffer) End() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.start + int64(len(b.data))
}

// TailOffset returns the offset where the last n lines begin. A trailing
// newline does not count as the start of an empty line.
func (b *outputBuffer) TailOffset(n int) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	i := len(b.data)

	if i > 0 && b.data[i-1] == '\n' {
		i--
	}

	for ; n > 0; n-- {
		j := bytes.LastIndexByte(b.data[:i], '\n')

		if j < 0 {
			return b.start
		}

		i = j
	}

	return b.start + int64(i) + 1
}

type readOptions struct {
	From    int64
	WaitFor *regexp.Regexp
	Idle    time.Duration
	Timeout time.Duration
	Max     int
}

type readResult struct {
	Output    []byte
	Next      int64
	Truncated bool
	Matched   bool
	TimedOut  bool
	Closed    bool
}

// Read returns the output from opts.From onward.
//
// With WaitFor it blocks until the pattern appears in that output, and with
// Idle until no output has arrived for that long, measured from the later of
// the call and the last write. Without either it returns at once. Waiting ends
// early when the buffer is closed or Timeout passes. When there is more than
// Max bytes to return, the oldest part is dropped and Truncated is set.
func (b *outputBuffer) Read(ctx context.Context, opts readOptions) readResult {
	began := time.Now()
	deadline := began.Add(opts.Timeout)

	for {
		b.mu.Lock()
		res, ch, wake := b.check(opts, began, deadline)
		b.mu.Unlock()

		if ch == nil {
			return res
		}

		timer := time.NewTimer(time.Until(wake))

		select {
		case <-ch:
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()

			b.mu.Lock()
			res = b.result(opts)
			b.mu.Unlock()

			res.TimedOut = true

			return res
		}

		timer.Stop()
	}
}

// check decides whether Read is done. When it is not, it returns the channel
// that signals new output and the time to look again regardless.
func (b *outputBuffer) check(opts readOptions, began, deadline time.Time) (readResult, <-chan struct{}, time.Time) {
	res := b.result(opts)
	now := time.Now()
	wake := deadline

	switch {
	case opts.WaitFor != nil:
		if opts.WaitFor.Match(b.data[b.clamp(opts.From)-b.start:]) {
			res.Matched = true
			return res, nil, time.Time{}
		}
	case opts.Idle > 0:
		quietSince := began

		if b.lastWrite.After(quietSince) {
			quietSince = b.lastWrite
		}

		settled := quietSince.Add(opts.Idle)

		if !now.Before(settled) {
			return res, nil, time.Time{}
		}

		if settled.Before(wake) {
			wake = settled
		}
	default:
		return res, nil, time.Time{}
	}

	if b.closed {
		res.Closed = true
		return res, nil, time.Time{}
	}

	if !now.Before(deadline) {
		res.TimedOut = true
		return res, nil, time.Time{}
	}

	return res, b.changed, wake
}

func (b *outputBuffer) clamp(from int64) int64 {
	end := b.start + int64(len(b.data))

	if from < b.start {
		return b.start
	}

	if from > end {
		return end
	}

	return from
}

func (b *outputBuffer) result(opts readOptions) readResult {
	from := b.clamp(opts.From)
	out := b.data[from-b.start:]
	res := readResult{
		Next:      b.start + int64(len(b.data)),
		Truncated: from > opts.From,
		Closed:    b.closed,
	}

	if opts.Max > 0 && len(out) > opts.Max {
		out = out[len(out)-opts.Max:]
		res.Truncated = true

		// Cutting the head may split a character; skip what is left of it.
		for len(out) > 0 && !utf8.RuneStart(out[0]) {
			out = out[1:]
		}
	}

	// A character still being written is held back for the next read, so
	// that neither read returns half of it.
	if !b.closed {
		if n := incompleteRuneLen(out); n > 0 {
			out = out[:len(out)-n]
			res.Next -= int64(n)
		}
	}

	res.Output = bytes.Clone(out)

	return res
}

// incompleteRuneLen returns the length of the partial UTF-8 character at the
// end of p, or 0 if p ends on a character boundary.
func incompleteRuneLen(p []byte) int {
	for i := len(p) - 1; i >= 0 && i >= len(p)-utf8.UTFMax; i-- {
		if utf8.RuneStart(p[i]) {
			if utf8.FullRune(p[i:]) {
				return 0
			}

			return len(p) - i
		}
	}

	return 0
}
