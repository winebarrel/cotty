package cotty

import (
	"fmt"
	"strings"
)

var namedKeys = map[string]string{
	"enter":     "\r",
	"tab":       "\t",
	"esc":       "\x1b",
	"escape":    "\x1b",
	"space":     " ",
	"backspace": "\x7f",
	"delete":    "\x1b[3~",
	"up":        "\x1b[A",
	"down":      "\x1b[B",
	"right":     "\x1b[C",
	"left":      "\x1b[D",
	"home":      "\x1b[H",
	"end":       "\x1b[F",
	"pageup":    "\x1b[5~",
	"pagedown":  "\x1b[6~",
}

// keyBytes returns what a terminal sends for the named key. Names are case
// insensitive, and a control key is written ctrl-x or ctrl+x.
func keyBytes(name string) ([]byte, error) {
	key := strings.ToLower(strings.TrimSpace(name))

	if s, ok := namedKeys[key]; ok {
		return []byte(s), nil
	}

	for _, prefix := range []string{"ctrl-", "ctrl+"} {
		rest, ok := strings.CutPrefix(key, prefix)

		if !ok || len(rest) != 1 {
			continue
		}

		c := rest[0]

		switch {
		case c >= 'a' && c <= 'z':
			return []byte{c - 'a' + 1}, nil
		case c == '@' || c == ' ':
			return []byte{0}, nil
		case c >= '[' && c <= '_':
			return []byte{c - '@'}, nil
		}
	}

	return nil, fmt.Errorf("unknown key: %q", name)
}
