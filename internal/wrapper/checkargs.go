package wrapper

import (
	"errors"
	"os"
	"strings"

	"github.com/Masterminds/semver/v3"
)

func checkStateCommand(args []string, version *semver.Version) error {
	versionImport, _ := semver.NewConstraint(">= 1.5.0")
	versionMoved, _ := semver.NewConstraint(">= 1.1.0")
	versionRemoved, _ := semver.NewConstraint(">= 1.7.0")
	STATE_COMMAND_VAR := "TF_DEMUX_ALLOW_STATE_COMMANDS"

	if checkArgsExists(args, "import") >= 0 &&
		versionImport.Check(version) {
		if allowStateCommand(STATE_COMMAND_VAR) {
			return nil
		} else {
			return errors.New("need set TF_DEMUX_ALLOW_STATE_COMMANDS=true for the 'import' command. Consider using Terraform configuration import block instead")
		}
	}

	if checkArgsExists(args, "state") >= 0 &&
		checkArgsExists(args, "mv") >= 0 &&
		versionMoved.Check(version) {
		if allowStateCommand(STATE_COMMAND_VAR) {
			return nil
		} else {
			return errors.New("need set TF_DEMUX_ALLOW_STATE_COMMANDS=true for the 'state mv' command. Consider using Terraform configuration moved block instead")
		}
	}

	if checkArgsExists(args, "state") >= 0 &&
		checkArgsExists(args, "rm") >= 0 &&
		versionRemoved.Check(version) {
		if allowStateCommand(STATE_COMMAND_VAR) {
			return nil
		} else {
			return errors.New("need set TF_DEMUX_ALLOW_STATE_COMMANDS=true for the 'state rm' command. Consider using Terraform configuration removed block instead")
		}
	}

	return nil
}

func checkArgsExists(args []string, cmd string) int {
	for i, arg := range args {
		if arg == cmd {
			return i
		}
	}
	return -1
}

func allowStateCommand(envVarName string) bool {
	validValues := []string{"1", "true", "yes"}
	value := strings.ToLower(os.Getenv(envVarName))
	for _, valid := range validValues {
		if value == valid {
			return true
		}
	}
	return false
}
