// Command testgen writes the synthetic Takeout corpus used in CI into a
// folder, for smoke tests of the built takeout binary.
package main

import (
	"fmt"
	"os"

	"github.com/asmodeoux/google-photos-takeout/internal/testgen"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: testgen <archives dir>")
		os.Exit(2)
	}
	paths, err := testgen.Corpus().Write(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, p := range paths {
		fmt.Println(p)
	}
}
