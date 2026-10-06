package cotty

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrefixFilter(t *testing.T) {
	tests := []struct {
		name     string
		in       []string
		forward  string
		commands string
	}{
		{"plain", []string{"ls\r"}, "ls\r", ""},
		{"toggle", []string{"a\x1dab"}, "ab", "a"},
		{"pause", []string{"\x1dp"}, "", "p"},
		{"help", []string{"\x1d?"}, "", "?"},
		{"literal prefix", []string{"\x1d\x1d"}, "\x1d", ""},
		{"unbound", []string{"x\x1dzy"}, "xy", ""},
		{"split", []string{"a\x1d", "a", "b"}, "ab", "a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f prefixFilter
			var forward, commands []byte

			for _, in := range tt.in {
				fw, cmds := f.Filter([]byte(in))
				forward = append(forward, fw...)
				commands = append(commands, cmds...)
			}

			assert.Equal(t, tt.forward, string(forward))
			assert.Equal(t, tt.commands, string(commands))
		})
	}
}
