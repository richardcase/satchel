// Command satchel installs, updates and removes agent skills.
package main

import (
	"os"

	"github.com/richardcase/satchel/internal/cli"
)

func main() { os.Exit(cli.Execute()) }
