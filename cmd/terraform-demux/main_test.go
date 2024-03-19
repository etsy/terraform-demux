package main

import (
	"testing"
)

func TestCheckStateCommand(t *testing.T) {
	t.Run("Valid state command with --force flag after state command", func(t *testing.T) {
		args := []string{"terraform", "state", "--force", "list"}
		err := checkStateCommand(args)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
	})

	t.Run("Valid state command with --force flag before state command", func(t *testing.T) {
		args := []string{"terraform", "--force", "state", "pull"}
		err := checkStateCommand(args)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
	})

	t.Run("Invalid state command without --force flag", func(t *testing.T) {
		args := []string{"terraform", "state", "list"}
		err := checkStateCommand(args)
		expectedError := "--force flag is required for the 'state' command"
		if err == nil || err.Error() != expectedError {
			t.Errorf("Expected error: %s, got: %v", expectedError, err)
		}
	})
}
