package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/KalebCole/partiful/internal/compose"
)

func main() {
	ctx := context.Background()
	server, err := compose.NewMCP(ctx, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "partiful-mcp startup failed")
		os.Exit(1)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	if err := server.ServeSignals(ctx, os.Stdin, os.Stdout, signals); err != nil {
		fmt.Fprintln(os.Stderr, "partiful-mcp server failed")
		os.Exit(1)
	}
}
