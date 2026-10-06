package cotty

// prefixKey is Ctrl-]. It leads the key bindings that talk to cotty itself
// instead of the program in the session.
const prefixKey = 0x1d

const (
	prefixToggleAgent = 'a'
	prefixTogglePause = 'p'
	prefixHelp        = '?'
)

// prefixFilter pulls prefix key bindings out of what the user types.
//
// The prefix and the key after it are never forwarded, except that pressing
// the prefix twice sends one literal Ctrl-]. A key with no binding is
// swallowed. The pending state is kept between calls because the two keys
// usually arrive in separate reads.
type prefixFilter struct {
	pending bool
}

// Filter returns the bytes to forward and the bound keys that were pressed.
func (f *prefixFilter) Filter(p []byte) (forward []byte, commands []byte) {
	forward = make([]byte, 0, len(p))

	for _, c := range p {
		switch {
		case f.pending:
			f.pending = false

			switch c {
			case prefixKey:
				forward = append(forward, c)
			case prefixToggleAgent, prefixTogglePause, prefixHelp:
				commands = append(commands, c)
			}
		case c == prefixKey:
			f.pending = true
		default:
			forward = append(forward, c)
		}
	}

	return forward, commands
}
