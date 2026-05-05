package main

import (
	"bytes"
	"log"
	"os"
	"runtime"

	"github.com/etsy/terraform-demux/internal/wrapper"
)

var (
	version = "v0.0.1+dev"
)

func main() {
	// When TF_DEMUX_LOG is unset, hold log output in a buffer so we can
	// replay it on stderr if something goes wrong. Otherwise the user only
	// sees the final error message and not the trace that led to it.
	var logBuf bytes.Buffer
	verboseLogging := os.Getenv("TF_DEMUX_LOG") != ""
	if verboseLogging {
		log.SetOutput(os.Stderr)
	} else {
		log.SetOutput(&logBuf)
	}

	arch := os.Getenv("TF_DEMUX_ARCH")
	if arch == "" {
		arch = runtime.GOARCH
	}

	log.Printf("terraform-demux version %s, using arch '%s'", version, arch)

	exitCode, err := wrapper.RunTerraform(os.Args[1:], arch)
	if err != nil {
		if !verboseLogging {
			os.Stderr.Write(logBuf.Bytes())
		}
		log.SetOutput(os.Stderr)
		log.Fatal("error: ", err)
	}

	os.Exit(exitCode)
}
