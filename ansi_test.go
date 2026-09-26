package cotty

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAnsiStripper(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello\n", "hello\n"},
		{"crlf", "a\r\nb\r\n", "a\nb\n"},
		{"tab", "a\tb", "a\tb"},
		{"sgr", "\x1b[1;31mred\x1b[0m", "red"},
		{"cursor", "\x1b[2J\x1b[H$ ", "$ "},
		{"private mode", "\x1b[?2004hprompt\x1b[?2004l", "prompt"},
		{"osc bel", "\x1b]0;title\x07text", "text"},
		{"osc st", "\x1b]2;title\x1b\\text", "text"},
		{"charset", "\x1b(Babc", "abc"},
		{"keypad", "\x1b=abc\x1b>", "abc"},
		{"backspace and bell", "ab\x08c\x07", "abc"},
		{"utf-8", "こんにちは\n", "こんにちは\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s ansiStripper
			assert.Equal(t, tt.want, string(s.Strip([]byte(tt.in))))
		})
	}
}

func TestAnsiStripperSplit(t *testing.T) {
	assert := assert.New(t)

	// A sequence cut in the middle by a read boundary is still removed.
	in := "a\x1b[1;31mb\x1b]0;t\x1b\\c"

	for i := range len(in) + 1 {
		var s ansiStripper
		got := string(s.Strip([]byte(in[:i]))) + string(s.Strip([]byte(in[i:])))
		assert.Equal("abc", got, "split at %d", i)
	}
}
