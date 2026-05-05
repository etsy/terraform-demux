package wrapper

import (
	"fmt"
	"os"
	"strings"

	"github.com/Masterminds/semver/v3"
)

const stateCommandVar = "TF_DEMUX_ALLOW_STATE_COMMANDS"

// State-command guard thresholds — the lowest Terraform version at which
// each command has a recommended config-block alternative. Initialized at
// package load so a typo in any constraint string is caught at startup
// rather than ignored via discarded errors at every call.
var (
	importStateCmdMin  = mustParseConstraint(">= 1.5.0")
	movedStateCmdMin   = mustParseConstraint(">= 1.1.0")
	removedStateCmdMin = mustParseConstraint(">= 1.7.0")
)

func mustParseConstraint(s string) *semver.Constraints {
	c, err := semver.NewConstraint(s)
	if err != nil {
		panic(fmt.Sprintf("invalid constraint %q: %v", s, err))
	}
	return c
}

func checkStateCommand(args []string, version *semver.Version) error {
	if allowStateCommand(stateCommandVar) {
		return nil
	}

	cmd, sub := terraformSubcommand(args)

	switch {
	case cmd == "import" && importStateCmdMin.Check(version):
		return refuseStateCommand("import", "import")
	case cmd == "state" && sub == "mv" && movedStateCmdMin.Check(version):
		return refuseStateCommand("state mv", "moved")
	case cmd == "state" && sub == "rm" && removedStateCmdMin.Check(version):
		return refuseStateCommand("state rm", "removed")
	}
	return nil
}

// terraformSubcommand returns the first and second positional arguments
// from args, treating any token that starts with "-" as a flag and
// skipping it. This catches the common "-flag=value" form. It does not
// reliably handle the "-flag value" (space-separated) form: a flag value
// would be misread as a positional. Terraform's CLI almost universally
// uses "-flag=value", so this is acceptable in practice.
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
