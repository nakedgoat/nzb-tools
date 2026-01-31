package main

import (
	"errors"
)

// decodeYEncLine decodes a single line of yEnc-encoded text into bytes.
// It handles the '=' escape mechanism and the offset of +42 used by yEnc.
func decodeYEncLine(line []byte) ([]byte, error) {
	if len(line) == 0 {
		return nil, nil
	}
	out := make([]byte, 0, len(line))
	for i := 0; i < len(line); {
		c := line[i]
		if c == '=' {
			if i+1 >= len(line) {
				return nil, errors.New("unterminated escape in yEnc line")
			}
			n := line[i+1]
			// Unescape: subtract 64 then remove the +42 offset
			v := (int(n) - 64 - 42) & 0xFF
			out = append(out, byte(v))
			i += 2
			continue
		}
		v := (int(c) - 42) & 0xFF
		out = append(out, byte(v))
		i++
	}
	return out, nil
}
