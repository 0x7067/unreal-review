package main

import (
	"fmt"
	"os"
)

func lastN(items []string, n int) []string {
	if n > len(items) {
		n = len(items)
	}
	return items[len(items)-n:]
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Println(usage())
	}
	for _, name := range args {
		fmt.Printf("%s=%d\n", name, lookup(name).value)
	}
	fmt.Println(lastN(args, 3))
}

func usage() string {
	return "usage: livetest NAME [NAME...]"
}
