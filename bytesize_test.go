package cotty

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestByteSize(t *testing.T) {
	tests := map[string]ByteSize{
		"100":    100,
		"100b":   100,
		"32KiB":  32 << 10,
		"32k":    32 << 10,
		"4 KB":   4 << 10,
		"1MiB":   1 << 20,
		"16M":    16 << 20,
		" 2mb  ": 2 << 20,
	}

	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			var b ByteSize
			require.NoError(t, b.UnmarshalText([]byte(in)))
			assert.Equal(t, want, b)
		})
	}
}

func TestByteSizeInvalid(t *testing.T) {
	for _, in := range []string{"", "0", "-1", "1GiB", "abc", "1.5M", "k", "99999999M"} {
		var b ByteSize
		assert.Error(t, b.UnmarshalText([]byte(in)), in)
	}
}
