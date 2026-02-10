package main

import (
	"context"
	"os"

	"github.com/PipeOpsHQ/agent-sdk-go/internal/cli"
)

func main() {
	ctx := context.Background()
	cli.Run(ctx, os.Args[1:])
}
