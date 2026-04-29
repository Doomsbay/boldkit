package util

import (
	"fmt"
	"os"
)

func Logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[boldkit] "+format+"\n", args...)
}
