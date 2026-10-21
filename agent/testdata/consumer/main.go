// Copied outside the checkout to exercise only the public Go API.
package main

import (
	"fmt"
	"os"
)

func main() {
	mode := "portable"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	var err error
	switch mode {
	case "portable":
		err = runPortable()
	case "gohost":
		err = runGoHost()
	default:
		err = fmt.Errorf("unknown mode %q", mode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
