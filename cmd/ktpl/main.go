// Command ktpl renders native Kubernetes manifests with cross-object Go templates.
package main

import (
	"os"

	"github.com/bricks-it/ktpl/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
