package cotty

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRedactRules(t *testing.T) {
	rules, err := parseRedactRules([]string{"hunter2", "/pass=\\S+/", "/", "//x"})
	require.NoError(t, err)
	require.Len(t, rules, 4)
	assert.Equal(t, "hunter2", string(rules[0].literal))
	assert.Equal(t, `pass=\S+`, rules[1].re.String())
	assert.Equal(t, "/", string(rules[2].literal))
	assert.Equal(t, "//x", string(rules[3].literal))

	rules, err = parseRedactRules([]string{"a", "", "b"})
	require.NoError(t, err)
	assert.Len(t, rules, 2)

	_, err = parseRedactRules([]string{"/(/"})
	assert.ErrorContains(t, err, "--redact #1: error parsing regexp")

	_, err = parseRedactRules([]string{"/x*/"})
	assert.EqualError(t, err, "--redact #1 matches an empty string")

	_, err = parseRedactRules([]string{"//"})
	assert.EqualError(t, err, "--redact #1 matches an empty string")
}

func TestRedactor(t *testing.T) {
	tests := []struct {
		name  string
		rules []string
		in    []string
		want  string
	}{
		{"no rules", nil, []string{"hunter2\n"}, "hunter2\n"},
		{"literal", []string{"hunter2"}, []string{"pw hunter2 ok\n"}, "pw [REDACTED] ok\n"},
		{"twice", []string{"ab"}, []string{"ab ab\n"}, "[REDACTED] [REDACTED]\n"},
		{"adjacent", []string{"ab"}, []string{"abab\n"}, "[REDACTED]\n"},
		{"split literal", []string{"hunter2"}, []string{"pw hun", "ter2 ok\n"}, "pw [REDACTED] ok\n"},
		{"typed literal", []string{"hunter2"}, []string{"h", "u", "n", "t", "e", "r", "2", "\n"}, "[REDACTED]\n"},
		{"not the keyword", []string{"hunter2"}, []string{"hun", "gry\n"}, "hungry\n"},
		{"held at end", []string{"hunter2"}, []string{"$ hun"}, "$ "},
		{"regexp", []string{`/pass=\S+/`}, []string{"x pass=abc y\n"}, "x [REDACTED] y\n"},
		{"regexp typed", []string{`/pass=\S+/`}, []string{"pass=", "a", "b", "c", " y\n"}, "pass=[REDACTED] y\n"},
		{"regexp anchored", []string{`/^secret$/`}, []string{"secret\nnot secret\n"}, "[REDACTED]\nnot secret\n"},
		{"no multiline", []string{"a\nb"}, []string{"a\nb\n"}, "a\nb\n"},
		{"multibyte", []string{"秘密"}, []string{"これは秘", "密です\n"}, "これは[REDACTED]です\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules, err := parseRedactRules(tt.rules)
			require.NoError(t, err)

			r := redactor{rules: rules}
			var out []byte

			for _, in := range tt.in {
				out = append(out, r.Redact([]byte(in))...)
			}

			assert.Equal(t, tt.want, string(out))
		})
	}
}

func TestRedactorFlushAndReset(t *testing.T) {
	rules, err := parseRedactRules([]string{"hunter2"})
	require.NoError(t, err)

	r := redactor{rules: rules}
	assert.Equal(t, "$ ", string(r.Redact([]byte("$ hun"))))
	assert.Equal(t, "hun", string(r.Flush()))
	assert.Nil(t, r.Flush())

	assert.Equal(t, "", string(r.Redact([]byte("hunt"))))
	r.Reset()
	assert.Equal(t, "er2\n", string(r.Redact([]byte("er2\n"))))
}
