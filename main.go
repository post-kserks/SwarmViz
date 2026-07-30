package main

import (
	"os"

	"github.com/swarmviz/swarmviz/pkg/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
// test change
