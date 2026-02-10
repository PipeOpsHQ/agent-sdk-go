package main

import (
	"context"
	"os"

	"github.com/PipeOpsHQ/agent-sdk-go/internal/cli"
)

func main() {
	cli.Run(context.Background(), os.Args[1:])
}
