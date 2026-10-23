// Command cmd writes internal/vm's tree_operand.go. It is run by go generate
// in internal/vm.
package main

import (
	"log"
	"os"

	"forge.harakara.site/littleisland/noe/third_party/go-quickjs/internal/vm/internal/treegen"
)

func main() {
	out, err := treegen.Generate()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("tree_operand.go", out, 0o644); err != nil {
		log.Fatal(err)
	}
}
