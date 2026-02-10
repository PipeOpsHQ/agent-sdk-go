package main

import (
	"context"
	"os"

	"github.com/PipeOpsHQ/agent-sdk-go/framework/internal/cli"
)

func main() {
	cli.Run(context.Background(), os.Args[1:])
}
