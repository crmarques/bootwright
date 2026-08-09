package cli

import (
	"bytes"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var safetyMatrixFlagExemptions = map[string]string{
	"apply/ask-become-pass":          "credential prompt, not an authorization; every matrix row already pins it off",
	"destroy/ask-become-pass":        "credential prompt, not an authorization; every matrix row already pins it off",
	"apply/verbose":                  "redaction escape hatch; pinned by the verbose-flag tests",
	"destroy/verbose":                "redaction escape hatch; pinned by the verbose-flag tests",
	"apply/trust-on-first-use":       "host-key trust; pinned by the ssh_trust_tofu tests",
	"destroy/recover-ceph-ownership": "pinned end to end by TestDestroyRecoverCephOwnershipValidatesAndEmitsConfirmedFSID",
}

func mutatingVerbCommands(t *testing.T) map[string]*cobra.Command {
	t.Helper()
	root := newRootCmd(bytes.NewReader(nil), &bytes.Buffer{}, &bytes.Buffer{})
	out := map[string]*cobra.Command{}
	for _, name := range []string{"apply", "destroy"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil || cmd == nil || cmd.Name() != name {
			t.Fatalf("root command has no %q verb: %v", name, err)
		}
		out[name] = cmd
	}
	return out
}

func safetyMatrixExercisedFlags() map[string]map[string]bool {
	exercised := map[string]map[string]bool{"apply": {}, "destroy": {}}
	for _, tc := range safetyMatrixCases() {
		args := stripLeadingGlobalFlags(tc.args)
		if len(args) == 0 || (args[0] != "apply" && args[0] != "destroy") {
			continue
		}
		verbFlags := exercised[args[0]]
		for _, arg := range tc.args {
			if !strings.HasPrefix(arg, "--") {
				continue
			}
			name := strings.TrimPrefix(arg, "--")
			if idx := strings.IndexByte(name, '='); idx >= 0 {
				name = name[:idx]
			}
			verbFlags[name] = true
		}
	}
	return exercised
}

func TestEveryApplyDestroyFlagIsExercisedByTheSafetyMatrix(t *testing.T) {
	exercised := safetyMatrixExercisedFlags()
	for verb, cmd := range mutatingVerbCommands(t) {
		var missing []string
		visit := func(f *pflag.Flag) {
			if _, exempt := safetyMatrixFlagExemptions[verb+"/"+f.Name]; exempt {
				return
			}
			if !exercised[verb][f.Name] {
				missing = append(missing, f.Name)
			}
		}
		cmd.Flags().VisitAll(visit)
		cmd.InheritedFlags().VisitAll(visit)
		sort.Strings(missing)
		for _, name := range missing {
			t.Errorf("`bootwright %s --%s` is registered but no safetyMatrixCases() row passes it; a flag on a mutating verb changes what the run does, so add a row asserting its verdict — or add a %q entry to safetyMatrixFlagExemptions naming the test that pins it instead", verb, name, verb+"/"+name)
		}
	}
}

func TestSafetyMatrixFlagExemptionsHoldOnlyLiveFlags(t *testing.T) {
	commands := mutatingVerbCommands(t)
	for key := range safetyMatrixFlagExemptions {
		verb, name, ok := strings.Cut(key, "/")
		if !ok {
			t.Errorf("safetyMatrixFlagExemptions key %q must be <verb>/<flag>", key)
			continue
		}
		cmd, known := commands[verb]
		if !known {
			t.Errorf("safetyMatrixFlagExemptions names verb %q, which is not a mutating verb", verb)
			continue
		}
		if cmd.Flags().Lookup(name) == nil && cmd.InheritedFlags().Lookup(name) == nil {
			t.Errorf("safetyMatrixFlagExemptions exempts `bootwright %s --%s`, which no longer exists; a dead exemption hides the next flag that lands ungated", verb, name)
		}
	}
}
