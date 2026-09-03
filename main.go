package main

import (
	"os"

	"vaulty-keeper/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
