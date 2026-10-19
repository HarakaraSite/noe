// This program is copied outside the NOE module and compiled as an independent
// Go consumer. It intentionally imports no NOE internal packages.
package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if os.Getenv("NOE_CONSUMER_SELF_REEXEC_MARKER") != "" && len(os.Args) > 1 && os.Args[1] == "run" {
		_ = os.WriteFile(os.Getenv("NOE_CONSUMER_SELF_REEXEC_MARKER"), []byte("consumer was executed as NOE"), 0600)
		fmt.Fprintln(os.Stderr, "step72 consumer self-reexecution marker")
		os.Exit(93)
	}
	if len(os.Args) < 2 {
		fail(errors.New("mode is required: portable, reference, or live"))
	}
	mode := os.Args[1]
	var err error
	switch mode {
	case "portable":
		err = runPortable()
	case "reference":
		err = runReference(os.Args[2:])
	case "live":
		err = runLive(os.Args[2:])
	default:
		err = fmt.Errorf("unknown mode %q: portable, reference, or live", mode)
	}
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
