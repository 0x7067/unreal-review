package main

import (
	"fmt"
	"os"
)

func lastN(items []string, n int) []string {
	return items[len(items)-n:]
}

func main() {
	args := os.Args[1:]
	for _, name := range args {
		fmt.Printf("%s=%d\n", name, lookup(name).value)
	}
	fmt.Println(lastN(args, 3))
}
