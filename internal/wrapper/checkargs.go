package wrapper

import (
	"fmt"
	"os"
	"strings"

	"github.com/Masterminds/semver/v3"
)

const stateCommandVar = "TF_DEMUX_ALLOW_STATE_COMMANDS"

func checkStateCommand(args []string, version *semver.Version) error {
	if allowStateCommand(stateCommandVar) {
		return nil
	}

	versionImport, _ := semver.NewConstraint(">= 1.5.0")
	versionMoved, _ := semver.NewConstraint(">= 1.1.0")
	versionRemoved, _ := semver.NewConstraint(">= 1.7.0")

	cmd, sub := terraformSubcommand(args)

	switch {
	case cmd == "import" && versionImport.Check(version):
		return refuseStateCommand("import", "import")
	case cmd == "state" && sub == "mv" && versionMoved.Check(version):
		return refuseStateCommand("state mv", "moved")
	case cmd == "state" && sub == "rm" && versionRemoved.Check(version):
		return refuseStateCommand("state rm", "removed")
	}
	return nil
}

// terraformSubcommand returns the first and second positional arguments,
// ignoring anything that looks like a flag. This avoids false positives where
// a flag value contains a literal like "import" or "mv" (e.g. -var=action=mv).
// It does not handle the rare "-flag value" form, but Terraform's CLI almost
// universally uses "-flag=value".
func terraformSubcommand(args []string) (cmd, sub string) {
	var positional []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		positional = append(positional, a)
	}
	if len(positional) > 0 {
		cmd = positional[0]
	}
	if len(positional) > 1 {
		sub = positional[1]
	}
	return
}

func refuseStateCommand(cmd, suggestion string) error {
	return fmt.Errorf("refusing to execute '%s' command - use a '%s' configuration block instead, or set %s=true", cmd, suggestion, stateCommandVar)
}

func allowStateCommand(envVarName string) bool {
	value := strings.ToLower(os.Getenv(envVarName))
	switch value {
	case "1", "true", "yes":
		return true
	}
	return false
}
