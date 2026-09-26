package cotty

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestOutputBufferReadImmediate(t *testing.T) {
	assert := assert.New(t)
	b := newOutputBuffer(1024)
	b.Write([]byte("hello\nworld\n"))

	res := b.Read(context.Background(), readOptions{From: 6})
	assert.Equal("world\n", string(res.Output))
	assert.EqualValues(12, res.Next)
	assert.False(res.Truncated)

	res = b.Read(context.Background(), readOptions{From: res.Next})
	assert.Empty(res.Output)
	assert.EqualValues(12, res.Next)
}

func TestOutputBufferLimit(t *testing.T) {
	assert := assert.New(t)
	b := newOutputBuffer(4)
	b.Write([]byte("abcdef"))

	// Output older than the limit is gone, and a reader that wanted it is
	// told so.
	res := b.Read(context.Background(), readOptions{From: 0})
	assert.Equal("cdef", string(res.Output))
	assert.EqualValues(6, res.Next)
	assert.True(res.Truncated)
	assert.EqualValues(6, b.End())
}

func TestOutputBufferMax(t *testing.T) {
	assert := assert.New(t)
	b := newOutputBuffer(1024)
	b.Write([]byte("abcdef"))

	res := b.Read(context.Background(), readOptions{From: 0, Max: 2})
	assert.Equal("ef", string(res.Output))
	assert.EqualValues(6, res.Next)
	assert.True(res.Truncated)
}

func TestOutputBufferFromBeyondEnd(t *testing.T) {
	assert := assert.New(t)
	b := newOutputBuffer(1024)
	b.Write([]byte("abc"))

	res := b.Read(context.Background(), readOptions{From: 100})
	assert.Empty(res.Output)
	assert.EqualValues(3, res.Next)
}

func TestOutputBufferTailOffset(t *testing.T) {
	assert := assert.New(t)
	b := newOutputBuffer(1024)
	assert.EqualValues(0, b.TailOffset(3))

	b.Write([]byte("1\n2\n3\n4\n"))
	assert.EqualValues(4, b.TailOffset(2))
	assert.EqualValues(0, b.TailOffset(10))

	b.Write([]byte("$ "))
	assert.EqualValues(6, b.TailOffset(2))
}

func TestOutputBufferWaitFor(t *testing.T) {
	assert := assert.New(t)
	b := newOutputBuffer(1024)
	b.Write([]byte("booting\n"))

	go func() {
		time.Sleep(50 * time.Millisecond)
		b.Write([]byte("login: "))
	}()

	res := b.Read(context.Background(), readOptions{
		From:    0,
		WaitFor: regexp.MustCompile(`login:`),
		Timeout: 5 * time.Second,
	})
	assert.True(res.Matched)
	assert.False(res.TimedOut)
	assert.Equal("booting\nlogin: ", string(res.Output))
}

func TestOutputBufferWaitForTimeout(t *testing.T) {
	assert := assert.New(t)
	b := newOutputBuffer(1024)
	b.Write([]byte("booting\n"))

	res := b.Read(context.Background(), readOptions{
		From:    0,
		WaitFor: regexp.MustCompile(`login:`),
		Timeout: 50 * time.Millisecond,
	})
	assert.False(res.Matched)
	assert.True(res.TimedOut)
	assert.Equal("booting\n", string(res.Output))
}

func TestOutputBufferWaitForClosed(t *testing.T) {
	assert := assert.New(t)
	b := newOutputBuffer(1024)

	go func() {
		time.Sleep(50 * time.Millisecond)
		b.Write([]byte("bye\n"))
		b.Close()
	}()

	res := b.Read(context.Background(), readOptions{
		WaitFor: regexp.MustCompile(`never`),
		Timeout: 5 * time.Second,
	})
	assert.True(res.Closed)
	assert.False(res.TimedOut)
	assert.Equal("bye\n", string(res.Output))

	// Writes after close are kept but wake nobody.
	b.Write([]byte("late"))
	assert.EqualValues(8, b.End())
}

func TestOutputBufferIdle(t *testing.T) {
	assert := assert.New(t)
	b := newOutputBuffer(1024)

	go func() {
		for range 3 {
			time.Sleep(20 * time.Millisecond)
			b.Write([]byte("x"))
		}
	}()

	began := time.Now()
	res := b.Read(context.Background(), readOptions{
		Idle:    100 * time.Millisecond,
		Timeout: 5 * time.Second,
	})
	assert.Equal("xxx", string(res.Output))
	assert.False(res.TimedOut)
	assert.GreaterOrEqual(time.Since(began), 160*time.Millisecond)
}

func TestOutputBufferIdleTimeout(t *testing.T) {
	assert := assert.New(t)
	b := newOutputBuffer(1024)
	stop := make(chan struct{})
	defer close(stop)

	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
				b.Write([]byte("x"))
			}
		}
	}()

	res := b.Read(context.Background(), readOptions{
		Idle:    100 * time.Millisecond,
		Timeout: 100 * time.Millisecond,
	})
	assert.True(res.TimedOut)
	assert.NotEmpty(res.Output)
}

func TestOutputBufferContextCanceled(t *testing.T) {
	assert := assert.New(t)
	b := newOutputBuffer(1024)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	res := b.Read(ctx, readOptions{
		WaitFor: regexp.MustCompile(`never`),
		Timeout: 5 * time.Second,
	})
	assert.True(res.TimedOut)
}

func TestOutputBufferPartialRune(t *testing.T) {
	assert := assert.New(t)
	b := newOutputBuffer(1024)
	a := []byte("あい")

	// The second character has only arrived in part, so it is left for the
	// next read.
	b.Write(a[:4])
	res := b.Read(context.Background(), readOptions{})
	assert.Equal("あ", string(res.Output))
	assert.EqualValues(3, res.Next)

	b.Write(a[4:])
	res = b.Read(context.Background(), readOptions{From: res.Next})
	assert.Equal("い", string(res.Output))
	assert.EqualValues(6, res.Next)
}

func TestOutputBufferMaxSplitsRune(t *testing.T) {
	assert := assert.New(t)
	b := newOutputBuffer(1024)
	b.Write([]byte("あい"))

	res := b.Read(context.Background(), readOptions{Max: 4})
	assert.Equal("い", string(res.Output))
	assert.True(res.Truncated)
}
