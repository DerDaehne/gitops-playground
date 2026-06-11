// Command gop is the Go port of the gitops-playground CLI.
package main

import (
	"fmt"
	"os"

	"github.com/cloudogu/gitops-playground/go/internal/cli"
)

func main() {
	rc, err := cli.Execute(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(int(rc))
}
