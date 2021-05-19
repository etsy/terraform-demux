package main

import (
	"io/ioutil"
	"log"
	"os"

	"github.com/etsy/terraform-demux/internal/wrapper"
)

var (
	version = "v0.0.1+dev"
)

func main() {
	if os.Getenv("DEMUX_LOG") == "" {
		log.SetOutput(ioutil.Discard)
	}

	log.Printf("terraform-demux version %s", version)

	exitCode, err := wrapper.RunTerraform(os.Args[1:])

	if err != nil {
		log.SetOutput(os.Stderr)

		log.Fatal("error: ", err)
	}

	os.Exit(exitCode)
}
