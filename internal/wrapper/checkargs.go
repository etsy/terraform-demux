package wrapper

import (
	"fmt"
	"os"
	"strings"

	"github.com/Masterminds/semver/v3"
)

func checkStateCommand(args []string, version *semver.Version) error {
	versionImport, _ := semver.NewConstraint(">= 1.5.0")
	versionMoved, _ := semver.NewConstraint(">= 1.1.0")
	versionRemoved, _ := semver.NewConstraint(">= 1.7.0")
	STATE_COMMAND_VAR := "TF_DEMUX_ALLOW_STATE_COMMANDS"

	errorMsg := func(command string, suggestion string) error {
		return fmt.Errorf("refusing to execute '%s' command - use a '%s' configuration block instead, or set %s=true", command, suggestion, STATE_COMMAND_VAR)
	}

	if allowStateCommand(STATE_COMMAND_VAR) {
		return nil
	}

	if checkArgsExists(args, "import") >= 0 &&
		versionImport.Check(version) {
		return errorMsg("import", "import")
	}

	if checkArgsExists(args, "state") >= 0 &&
		checkArgsExists(args, "mv") >= 0 &&
		versionMoved.Check(version) {
		return errorMsg("state mv", "moved")
	}

	if checkArgsExists(args, "state") >= 0 &&
		checkArgsExists(args, "rm") >= 0 &&
		versionRemoved.Check(version) {
		return errorMsg("state rm", "removed")
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
