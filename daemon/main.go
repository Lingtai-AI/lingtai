package main

import (
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]

	if len(args) > 0 {
		switch args[0] {
		case "setup":
			fmt.Println("daemon setup — not yet implemented")
			return
		case "manage":
			fmt.Println("daemon manage — not yet implemented")
			return
		}
	}

	fmt.Println("daemon — not yet implemented")
}
