package wrapper

import (
	"slices"
	"testing"

	"github.com/Masterminds/semver/v3"
)

func TestCheckStateCommand(t *testing.T) {
	t.Run("Valid state import command with --force flag on 1.5.0", func(t *testing.T) {
		args := []string{"state", "import", "--force"}
		version, _ := semver.NewVersion("1.5.0")
		result, err := checkStateCommand(args, version)
		if err != nil || !slices.Equal(result, []string{"state", "import"}) {
			t.Errorf("Expected no error, got: %v, %v", err, result)
		}
	})

	t.Run("Valid state import command without --force flag on 1.4.7", func(t *testing.T) {
		args := []string{"state", "import"}
		version, _ := semver.NewVersion("1.4.7")
		result, err := checkStateCommand(args, version)
		if err != nil || !slices.Equal(result, []string{"state", "import"}) {
			t.Errorf("Expected no error, got: %v", err)
		}
	})

	t.Run("Invalid state import command without --force flag on 1.5.0", func(t *testing.T) {
		args := []string{"state", "import"}
		version, _ := semver.NewVersion("1.6.0")
		result, err := checkStateCommand(args, version)
		if err == nil {
			t.Errorf("Expected error, got: %v, %v", err, result)
		}
	})

	t.Run("Valid state mv command with --force flag on 1.6.0", func(t *testing.T) {
		args := []string{"state", "mv", "--force"}
		version, _ := semver.NewVersion("1.6.0")
		result, err := checkStateCommand(args, version)
		if err != nil || !slices.Equal(result, []string{"state", "mv"}) {
			t.Errorf("Expected no error, got: %v, %v", err, result)
		}
	})
}

func TestCheckArgsExists(t *testing.T) {
	t.Run("Check 'state import --force' command", func(t *testing.T) {
		args := []string{"state", "import", "--force"}
		result := checkArgsExists(args, "state")
		if result != 0 {
			t.Errorf("Expected 0, got: %v", result)
		}
		result = checkArgsExists(args, "import")
		if result != 1 {
			t.Errorf("Expected 1, got: %v", result)
		}
		result = checkArgsExists(args, "--force")
		if result != 2 {
			t.Errorf("Expected 2, got: %v", result)
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
