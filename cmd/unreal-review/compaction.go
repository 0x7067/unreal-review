package main

import (
	"fmt"
	"os"

	"github.com/0x7067/unreal-review/agent"
)

func compactionThreshold(model, spec, window string) (int64, error) {
	threshold, note, err := agent.CompactionThreshold(model, spec, window)
	if err != nil {
		return 0, err
	}
	if note != "" {
		fmt.Fprintf(os.Stderr, "unreal-review: %s\n", note)
	}
	return threshold, nil
}
