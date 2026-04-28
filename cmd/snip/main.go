package main

import (
	"os"

	snip "github.com/aaronflorey/snip"
)

func main() {
	os.Exit(snip.Run(os.Args))
}
