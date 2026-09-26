package cotty

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeyBytes(t *testing.T) {
	tests := map[string]string{
		"enter":    "\r",
		"Tab":      "\t",
		" esc ":    "\x1b",
		"up":       "\x1b[A",
		"pagedown": "\x1b[6~",
		"ctrl-c":   "\x03",
		"CTRL+D":   "\x04",
		"ctrl-a":   "\x01",
		"ctrl-z":   "\x1a",
		"ctrl-@":   "\x00",
		"ctrl-[":   "\x1b",
		"ctrl-\\":  "\x1c",
		"ctrl-]":   "\x1d",
		"ctrl-_":   "\x1f",
	}

	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := keyBytes(name)
			require.NoError(t, err)
			assert.Equal(t, want, string(got))
		})
	}
}

func TestKeyBytesUnknown(t *testing.T) {
	for _, name := range []string{"", "f13", "ctrl-", "ctrl-ab", "ctrl-1", "alt-x"} {
		_, err := keyBytes(name)
		assert.Error(t, err, name)
	}
}
