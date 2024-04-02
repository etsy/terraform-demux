package wrapper

import (
	"os"
	"testing"

	"github.com/Masterminds/semver/v3"
)

func TestCheckStateCommand(t *testing.T) {
	STATE_COMMAND_VAR := "TF_DEMUX_ALLOW_STATE_COMMANDS"
	t.Run("Valid state import command with TF_DEMUX_ALLOW_STATE_COMMANDS on 1.5.0", func(t *testing.T) {
		args := []string{"import", "--force"}
		version, _ := semver.NewVersion("1.5.0")
		os.Setenv(STATE_COMMAND_VAR, "true")
		err := checkStateCommand(args, version)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
	})

	t.Run("Valid state import command without TF_DEMUX_ALLOW_STATE_COMMANDS on 1.4.7", func(t *testing.T) {
		args := []string{"import"}
		version, _ := semver.NewVersion("1.4.7")
		os.Setenv(STATE_COMMAND_VAR, "true")
		err := checkStateCommand(args, version)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
	})

	t.Run("Invalid state import command without TF_DEMUX_ALLOW_STATE_COMMANDS on 1.5.0", func(t *testing.T) {
		args := []string{"import"}
		version, _ := semver.NewVersion("1.6.0")
		os.Setenv(STATE_COMMAND_VAR, "")
		err := checkStateCommand(args, version)
		if err == nil {
			t.Errorf("Expected error, got: %v", err)
		}
	})

	t.Run("Valid state mv command with TF_DEMUX_ALLOW_STATE_COMMANDS on 1.6.0", func(t *testing.T) {
		args := []string{"state", "mv", "--force"}
		version, _ := semver.NewVersion("1.6.0")
		os.Setenv(STATE_COMMAND_VAR, "true")
		err := checkStateCommand(args, version)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
	})
}

func TestCheckArgsExists(t *testing.T) {
	t.Run("Check 'import --force' command", func(t *testing.T) {
		args := []string{"import", "--force"}
		result := checkArgsExists(args, "import")
		if result != 0 {
			t.Errorf("Expected 0, got: %v", result)
		}
		result = checkArgsExists(args, "--force")
		if result != 1 {
			t.Errorf("Expected 1, got: %v", result)
		}
	})

	t.Run("Check 'state moved' command", func(t *testing.T) {
		args := []string{"state", "mv"}
		result := checkArgsExists(args, "state")
		if result != 0 {
			t.Errorf("Expected 0, got: %v", result)
		}
		result = checkArgsExists(args, "mv")
		if result != 1 {
			t.Errorf("Expected 1, got: %v", result)
		}
		result = checkArgsExists(args, "--force")
		if result != -1 {
			t.Errorf("Expected -1, got: %v", result)
		}
	})
}
