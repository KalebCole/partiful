package main

import (
	"context"
	"fmt"
	"os"

	"github.com/KalebCole/partiful/internal/compose"
)

func main() {
	info, err := os.Stdin.Stat()
	isTerminal := err == nil && info.Mode()&os.ModeCharDevice != 0
	adapter, err := compose.NewCLI(os.Stdin, os.Stdout, os.Stderr, isTerminal)
	if err != nil {
		fmt.Fprintln(os.Stderr, "partiful startup failed")
		os.Exit(10)
	}
	os.Exit(adapter.Run(context.Background(), os.Args[1:]))
}
