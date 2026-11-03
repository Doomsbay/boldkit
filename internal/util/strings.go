package util

import "strings"

func IsNull(b []byte) bool {
	switch len(b) {
	case 2:
		return b[0] == 'N' && b[1] == 'A'
	case 4:
		return (b[0] == 'N' && b[1] == 'o' && b[2] == 'n' && b[3] == 'e') ||
			(b[0] == 'N' && b[1] == 'U' && b[2] == 'L' && b[3] == 'L')
	}
	return false
}

func Normalize(value []byte) []byte {
	if IsNull(value) {
		return nil
	}
	return value
}

func SplitList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func SafeTag(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' {
			b.WriteByte(c)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
