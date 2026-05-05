package wrapper

import (
	"testing"

	"github.com/Masterminds/semver/v3"
)

func TestCheckStateCommand(t *testing.T) {
	t.Run("import allowed when env var set", func(t *testing.T) {
		t.Setenv(stateCommandVar, "true")
		err := checkStateCommand([]string{"import", "module.foo", "id"}, mustVer(t, "1.5.0"))
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("import allowed on pre-1.5.0 even without env var", func(t *testing.T) {
		t.Setenv(stateCommandVar, "")
		err := checkStateCommand([]string{"import"}, mustVer(t, "1.4.7"))
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("import refused on 1.6.0 without env var", func(t *testing.T) {
		t.Setenv(stateCommandVar, "")
		err := checkStateCommand([]string{"import", "module.foo", "id"}, mustVer(t, "1.6.0"))
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("state mv allowed when env var set", func(t *testing.T) {
		t.Setenv(stateCommandVar, "true")
		err := checkStateCommand([]string{"state", "mv", "--force"}, mustVer(t, "1.6.0"))
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("state mv refused on 1.1.0+ without env var", func(t *testing.T) {
		t.Setenv(stateCommandVar, "")
		err := checkStateCommand([]string{"state", "mv", "a", "b"}, mustVer(t, "1.6.0"))
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("state rm refused on 1.7.0+ without env var", func(t *testing.T) {
		t.Setenv(stateCommandVar, "")
		err := checkStateCommand([]string{"state", "rm", "module.foo"}, mustVer(t, "1.7.0"))
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	// H4 regression: a flag value that happens to contain "import", "mv",
	// or "rm" must not trigger the guard. The guard is supposed to match
	// only the actual subcommand.
	t.Run("flag value containing 'import' does not trip guard", func(t *testing.T) {
		t.Setenv(stateCommandVar, "")
		err := checkStateCommand([]string{"apply", "-var=action=import"}, mustVer(t, "1.6.0"))
		if err != nil {
			t.Errorf("expected no error for apply with -var=action=import, got: %v", err)
		}
	})

	t.Run("flag value containing 'mv' does not trip guard", func(t *testing.T) {
		t.Setenv(stateCommandVar, "")
		err := checkStateCommand(
			[]string{"plan", "-target=module.state.mv"},
			mustVer(t, "1.6.0"),
		)
		if err != nil {
			t.Errorf("expected no error for plan with -target=module.state.mv, got: %v", err)
		}
	})

	t.Run("'mv' before 'state' positionally does not trip guard", func(t *testing.T) {
		t.Setenv(stateCommandVar, "")
		// 'mv' is the subcommand here (made up), not 'state mv'.
		err := checkStateCommand([]string{"mv", "state"}, mustVer(t, "1.6.0"))
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})
}

func TestTerraformSubcommand(t *testing.T) {
	cases := []struct {
		args    []string
		wantCmd string
		wantSub string
	}{
		{[]string{"import", "module.foo", "id"}, "import", "module.foo"},
		{[]string{"-chdir=/tmp", "state", "mv", "a", "b"}, "state", "mv"},
		{[]string{"apply", "-var=action=import"}, "apply", ""},
		{[]string{"plan", "-target=module.state.mv"}, "plan", ""},
		{[]string{}, "", ""},
		{[]string{"-help"}, "", ""},
	}
	for _, tc := range cases {
		gotCmd, gotSub := terraformSubcommand(tc.args)
		if gotCmd != tc.wantCmd || gotSub != tc.wantSub {
			t.Errorf("terraformSubcommand(%v) = (%q, %q), want (%q, %q)",
				tc.args, gotCmd, gotSub, tc.wantCmd, tc.wantSub)
		}
	}
}

func mustVer(t *testing.T, s string) *semver.Version {
	t.Helper()
	v, err := semver.NewVersion(s)
	if err != nil {
		t.Fatalf("invalid version %q: %v", s, err)
	}
	return v
}
