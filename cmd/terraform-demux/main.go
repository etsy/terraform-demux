package main

import (
	"log"
	"os"
	"runtime"

	"github.com/etsy/terraform-demux/internal/wrapper"
)

var version = "v0.0.1+dev"

func main() {
	verbose := os.Getenv("TF_DEMUX_LOG") != ""
	flushLog := wrapper.SetupLogging(verbose, os.Stderr)

	arch := os.Getenv("TF_DEMUX_ARCH")
	if arch == "" {
		arch = runtime.GOARCH
	}

	log.Printf("terraform-demux version %s, using arch '%s'", version, arch)

	exitCode, err := wrapper.RunTerraform(os.Args[1:], arch)
	if err != nil {
		flushLog()
		log.SetOutput(os.Stderr)
		log.Fatal("error: ", err)
	}

	os.Exit(exitCode)
}
