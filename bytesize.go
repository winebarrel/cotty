package cotty

import (
	"fmt"
	"strconv"
	"strings"
)

// ByteSize is a positive number of bytes, written as a number with an
// optional unit: B, K, KB, KiB, M, MB or MiB. Every unit is a power of 1024.
type ByteSize int

var byteUnits = []struct {
	suffixes []string
	scale    int
}{
	{[]string{"kib", "kb", "k"}, 1 << 10},
	{[]string{"mib", "mb", "m"}, 1 << 20},
	{[]string{"b"}, 1},
}

func (b *ByteSize) UnmarshalText(text []byte) error {
	s := strings.ToLower(strings.TrimSpace(string(text)))
	num, scale := s, 1

unit:
	for _, u := range byteUnits {
		for _, suffix := range u.suffixes {
			if n, ok := strings.CutSuffix(s, suffix); ok {
				num, scale = strings.TrimSpace(n), u.scale
				break unit
			}
		}
	}

	n, err := strconv.Atoi(num)

	if err != nil || n <= 0 || n > int(^uint32(0)>>1)/scale {
		return fmt.Errorf("invalid size: %q", string(text))
	}

	*b = ByteSize(n * scale)

	return nil
}
