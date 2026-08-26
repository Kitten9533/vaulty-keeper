package main

import (
	"os"

	"ai-tools/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
