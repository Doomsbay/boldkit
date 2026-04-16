package main

import (
	"os"

	"github.com/Doomsbay/BoldKit/boldkit/cmd"
)

var version = "dev"

func main() {
	cmd.Execute(os.Args[1:], version)
}
