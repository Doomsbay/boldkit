package taxonomy

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func LoadTaxidMap(path string) (map[string]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open taxid.map: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()
	out := make(map[string]int, 1<<20)
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			fields = strings.Fields(line)
		}
		if len(fields) < 2 {
			continue
		}
		id := fields[0]
		taxid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		out[id] = taxid
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan taxid.map: %w", err)
	}
	if len(out) == 0 {
		return nil, errors.New("taxid.map is empty")
	}
	return out, nil
}
