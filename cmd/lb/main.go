// lb-58x
package main

import (
	"os"

	"github.com/lesliesrussell/lazybeads/internal/cli"
)

func main() {
	os.Exit(cli.Execute(cli.Options{}))
}
