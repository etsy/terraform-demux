package main

import (
	"log"
	"os"

	"github.com/etsy/terraform-demux/internal/wrapper"
)

func main() {
	exitCode, err := wrapper.RunTerraform(os.Args[1:])

	if err != nil {
		log.Fatal(err)
	}

	os.Exit(exitCode)
}
