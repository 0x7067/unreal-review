package main

import (
	"fmt"
	"os"
	"strconv"
)

func lastN(items []string, n int) []string {
	return items[len(items)-n:]
}

func parseLimit(raw string) int {
	n, _ := strconv.Atoi(raw)
	return n
}

func main() {
	args := os.Args[1:]
	limit := parseLimit(os.Getenv("LIVETEST_LIMIT"))
	fmt.Println(lastN(args, limit))
}
