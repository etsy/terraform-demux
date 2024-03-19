package main

import (
	"errors"
	"io"
	"log"
	"os"
	"runtime"

	"github.com/etsy/terraform-demux/internal/wrapper"
)

var (
	version = "v0.0.1+dev"
)

func main() {
	if os.Getenv("TF_DEMUX_LOG") == "" {
		log.SetOutput(io.Discard)
	}

	arch := os.Getenv("TF_DEMUX_ARCH")

	if arch == "" {
		arch = runtime.GOARCH
	}

	log.Printf("terraform-demux version %s, using arch '%s'", version, arch)

	if err := checkStateCommand(os.Args); err != nil {
		log.SetOutput(os.Stderr)
		log.Fatal("error: ", err)
	}

	exitCode, err := wrapper.RunTerraform(os.Args[1:], arch)

	if err != nil {
		log.SetOutput(os.Stderr)

		log.Fatal("error: ", err)
	}

	os.Exit(exitCode)
}

func checkStateCommand(args []string) error {
	if checkArgsExists(args, "state") && !checkArgsExists(args, "--force") {
		return errors.New("--force flag is required for the 'state' command")
	}
	return nil
}

func checkArgsExists(args []string, cmd string) bool {
	for _, arg := range args {
		if arg == cmd {
			return true
		}
	}
	return false
}
