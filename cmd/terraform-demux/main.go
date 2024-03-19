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

	newArgs, err := checkStateCommand(os.Args)
	if err != nil {
		log.SetOutput(os.Stderr)
		log.Fatal("error: ", err)
	}

	exitCode, err := wrapper.RunTerraform(newArgs[1:], arch)

	if err != nil {
		log.SetOutput(os.Stderr)

		log.Fatal("error: ", err)
	}

	os.Exit(exitCode)
}

func checkStateCommand(args []string) ([]string, error) {
	if checkArgsExists(args, "state") > 0 {
		force_pos := checkArgsExists(args, "--force")
		if force_pos > 0 {
			return append(args[:force_pos], args[force_pos+1:]...), nil
		} else {
			return args, errors.New("--force flag is required for the 'state' command. Consider using Terraform configuration blocks (moved, import) instead")
		}
	}
	return args, nil
}

func checkArgsExists(args []string, cmd string) int {
	for i, arg := range args {
		if arg == cmd {
			return i
		}
	}
	return -1
}
