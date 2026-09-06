package util

func PrepareSeqBytes(dst []byte, src []byte) []byte {
	dst = dst[:0]
	for _, c := range src {
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			continue
		case c >= 'a' && c <= 'z':
			dst = append(dst, c-32)
		default:
			dst = append(dst, c)
		}
	}
	return dst
}

func IndexOfBytes(values [][]byte, name string) int {
	for i, v := range values {
		if string(v) == name {
			return i
		}
	}
	return -1
}

func SanitizeMarkerBytes(dst []byte, src []byte) string {
	dst = dst[:0]
	dst = append(dst, src...)
	for i := 0; i < len(dst); i++ {
		c := dst[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' {
			continue
		}
		dst[i] = '_'
	}
	return string(dst)
}
