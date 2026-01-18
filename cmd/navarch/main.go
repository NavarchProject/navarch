package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("Navarch CLI - Hello World")
	if len(os.Args) > 1 {
		fmt.Printf("Command: %s\n", os.Args[1])
	}
}

