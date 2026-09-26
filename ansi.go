package cotty

const (
	ansiNormal = iota
	ansiEsc
	ansiEscIntermediate
	ansiCSI
	ansiString
	ansiStringEsc
)

// ansiStripper removes escape sequences and control characters from terminal
// output so that what is left reads as plain text.
//
// A sequence may be split across reads, so the parser state is kept between
// calls. Only newlines and tabs survive among the control characters. Carriage
// returns and backspaces are dropped rather than applied, which leaves
// redrawn lines (progress bars, line editing) looking a little noisy but never
// loses text.
type ansiStripper struct {
	state int
}

func (s *ansiStripper) Strip(p []byte) []byte {
	out := make([]byte, 0, len(p))

	for _, c := range p {
		switch s.state {
		case ansiNormal:
			switch {
			case c == 0x1b:
				s.state = ansiEsc
			case c == '\n' || c == '\t':
				out = append(out, c)
			case c < 0x20 || c == 0x7f:
				// Other control characters have no textual meaning.
			default:
				out = append(out, c)
			}
		case ansiEsc:
			switch {
			case c == '[':
				s.state = ansiCSI
			case c == ']' || c == 'P' || c == 'X' || c == '^' || c == '_':
				// OSC, DCS, SOS, PM and APC all run until a string terminator.
				s.state = ansiString
			case c >= 0x20 && c <= 0x2f:
				s.state = ansiEscIntermediate
			default:
				s.state = ansiNormal
			}
		case ansiEscIntermediate:
			if c < 0x20 || c > 0x2f {
				s.state = ansiNormal
			}
		case ansiCSI:
			if c >= 0x40 && c <= 0x7e {
				s.state = ansiNormal
			}
		case ansiString:
			switch c {
			case 0x07:
				s.state = ansiNormal
			case 0x1b:
				s.state = ansiStringEsc
			}
		case ansiStringEsc:
			if c == '\\' {
				s.state = ansiNormal
			} else {
				s.state = ansiString
			}
		}
	}

	return out
}
