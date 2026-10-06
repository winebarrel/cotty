package cotty

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// redactMark is shown in place of what a rule hides.
const redactMark = "[REDACTED]"

// redactRule is a keyword to hide from the agent and the log.
type redactRule struct {
	literal []byte
	re      *regexp.Regexp
}

// parseRedactRules parses the --redact values. A value between slashes is a
// regular expression; anything else is matched as it is. Empty values, such as
// blank lines in COTTY_REDACT, are skipped.
func parseRedactRules(values []string) ([]redactRule, error) {
	rules := make([]redactRule, 0, len(values))

	for i, v := range values {
		var rule redactRule
		num := i + 1

		switch {
		case v == "":
			continue
		case len(v) >= 2 && strings.HasPrefix(v, "/") && strings.HasSuffix(v, "/"):
			re, err := regexp.Compile(v[1 : len(v)-1])

			if err != nil {
				return nil, fmt.Errorf("--redact #%d: %w", num, err)
			}

			if re.MatchString("") {
				return nil, fmt.Errorf("--redact #%d matches an empty string", num)
			}

			rule.re = re
		default:
			rule.literal = []byte(v)
		}

		rules = append(rules, rule)
	}

	return rules, nil
}

// redactor replaces what the rules match in plain-text output. A match never
// spans lines.
//
// Output arrives in pieces, so the current line is kept and the rules are run
// over all of it each time. A keyword cut off at the end of a piece is held
// back until the rest arrives. A regular expression cannot be held back that
// way: text it matches only once more of the line arrives is hidden from that
// point on, and what was passed on before stays as it is.
type redactor struct {
	rules []redactRule

	// line is the part of the current line already passed on, and held the
	// part after it held back. masked tells whether line ends inside a match.
	line   []byte
	held   []byte
	masked bool
}

// Redact returns p with the matches replaced.
func (r *redactor) Redact(p []byte) []byte {
	if len(r.rules) == 0 {
		return p
	}

	data := append(r.held, p...)
	r.held = nil
	var out []byte

	for {
		i := bytes.IndexByte(data, '\n')

		if i < 0 {
			break
		}

		out = r.pass(out, data[:i], 0)
		out = append(out, '\n')
		r.line = r.line[:0]
		r.masked = false
		data = data[i+1:]
	}

	if len(data) > 0 {
		out = r.pass(out, data, r.holdLen(data))
	}

	return out
}

// Flush passes on what is held back, as the output has ended or is being cut.
func (r *redactor) Flush() []byte {
	if len(r.held) == 0 {
		return nil
	}

	held := r.held
	r.held = nil

	return r.pass(nil, held, 0)
}

// Reset forgets the current line, for when the output is cut.
func (r *redactor) Reset() {
	r.line = nil
	r.held = nil
	r.masked = false
}

// pass appends seg to the current line and passes on all of it but the last
// hold bytes, with the matches replaced.
func (r *redactor) pass(out, seg []byte, hold int) []byte {
	full := append(r.line, seg...)
	masked := r.match(full)
	end := len(full) - hold

	for i := len(r.line); i < end; i++ {
		switch {
		case !masked[i]:
			out = append(out, full[i])
			r.masked = false
		case !r.masked:
			// A match that goes on from what was passed on before is
			// already marked there.
			out = append(out, redactMark...)
			r.masked = true
		}
	}

	r.line = full[:end]
	r.held = bytes.Clone(full[end:])

	return out
}

// match returns, for each byte of line, whether a rule covers it.
func (r *redactor) match(line []byte) []bool {
	masked := make([]bool, len(line))

	cover := func(from, to int) {
		for i := from; i < to; i++ {
			masked[i] = true
		}
	}

	for _, rule := range r.rules {
		if rule.re != nil {
			for _, loc := range rule.re.FindAllIndex(line, -1) {
				cover(loc[0], loc[1])
			}

			continue
		}

		for off := 0; ; {
			i := bytes.Index(line[off:], rule.literal)

			if i < 0 {
				break
			}

			cover(off+i, off+i+len(rule.literal))
			off += i + 1
		}
	}

	return masked
}

// holdLen returns how much of the end of seg to hold back: the longest part
// that could be the start of a keyword, unless it is already hidden.
func (r *redactor) holdLen(seg []byte) int {
	full := append(bytes.Clone(r.line), seg...)
	hold := 0

	for _, rule := range r.rules {
		for n := min(len(rule.literal)-1, len(seg)); n > hold; n-- {
			if bytes.HasSuffix(full, rule.literal[:n]) {
				hold = n
				break
			}
		}
	}

	return hold
}
