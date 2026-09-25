package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"unreal-review/internal/review"
)

func runAgentTool(tool review.Tool, args []string) error {
	var stdin []byte
	if stat, err := os.Stdin.Stat(); err == nil && stat.Mode()&os.ModeCharDevice == 0 {
		stdin, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read standard input: %w", err)
		}
	}
	out, err := tool.Run(context.Background(), args, string(stdin))
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(os.Stdout, out)
	return nil
}
