package wrapper

import (
	"errors"

	"github.com/Masterminds/semver/v3"
)

func checkStateCommand(args []string, version *semver.Version) ([]string, error) {
	versionImport, _ := semver.NewConstraint(">= 1.5.0")
	versionMoved, _ := semver.NewConstraint(">= 1.1.0")
	versionImport.Check(version)
	if checkArgsExists(args, "state") >= 0 &&
		checkArgsExists(args, "import") >= 0 &&
		versionImport.Check(version) {
		force_pos := checkArgsExists(args, "--force")
		if force_pos > 0 {
			return append(args[:force_pos], args[force_pos+1:]...), nil
		} else {
			return args, errors.New("--force flag is required for the 'state import' command. Consider using Terraform configuration import block instead")
		}
	}

	if checkArgsExists(args, "state") >= 0 &&
		checkArgsExists(args, "mv") >= 0 &&
		versionMoved.Check(version) {
		force_pos := checkArgsExists(args, "--force")
		if force_pos > 0 {
			return append(args[:force_pos], args[force_pos+1:]...), nil
		} else {
			return args, errors.New("--force flag is required for the 'state mv' command. Consider using Terraform configuration moved block instead")
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
