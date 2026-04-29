package util

func Field(fields [][]byte, idx int) []byte {
	if idx < 0 || idx >= len(fields) {
		return nil
	}
	return fields[idx]
}

func MaxIndex(values ...int) int {
	max := -1
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	return max
}
