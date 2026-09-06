package taxonomy

func HasAllRanks(lineage map[string]string, required []string) bool {
	if len(required) == 0 {
		return true
	}
	for _, rank := range required {
		if rank == "" {
			continue
		}
		if lineage[rank] == "" {
			return false
		}
	}
	return true
}
