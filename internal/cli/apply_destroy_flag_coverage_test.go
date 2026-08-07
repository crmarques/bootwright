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

func safetyMatrixExercisedFlags() map[string]bool {
	exercised := map[string]bool{}
	for _, tc := range safetyMatrixCases() {
		for _, arg := range tc.args {
			if !strings.HasPrefix(arg, "--") {
				continue
			}
			name := strings.TrimPrefix(arg, "--")
			if idx := strings.IndexByte(name, '='); idx >= 0 {
				name = name[:idx]
			}
			exercised[name] = true
		}
	}
	return exercised
}

func TestEveryApplyDestroyFlagIsExercisedByTheSafetyMatrix(t *testing.T) {
	exercised := safetyMatrixExercisedFlags()
	for verb, cmd := range mutatingVerbCommands(t) {
		var missing []string
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if _, exempt := safetyMatrixFlagExemptions[verb+"/"+f.Name]; exempt {
				return
			}
			if !exercised[f.Name] {
				missing = append(missing, f.Name)
			}
		})
		sort.Strings(missing)
		for _, name := range missing {
			t.Errorf("`bootwright %s --%s` is registered but no safetyMatrixCases() row passes it; a flag on a mutating verb changes what the run does, so add a row asserting its verdict — or add a %q entry to safetyMatrixFlagExemptions naming the test that pins it instead", verb, name, verb+"/"+name)
		}
	}
}

func TestRefusalRemedyCarriesTheRunSelection(t *testing.T) {
	cases := []struct {
		name      string
		selection runSelection
		want      []string
		deny      []string
	}{{
		name:      "machine-scoped destroy",
		selection: runSelection{machines: "dc1-worker-1"},
		want:      []string{"--machines dc1-worker-1"},
		deny:      []string{"--clusters"},
	}, {
		name:      "staged cluster-scoped apply",
		selection: runSelection{stage: "deps", through: "base", clusters: "dc1-ocp"},
		want:      []string{"--stage deps", "--through base", "--clusters dc1-ocp"},
		deny:      []string{"--machines"},
	}, {
		name:      "unscoped run",
		selection: runSelection{},
		deny:      []string{"--clusters", "--machines", "--stage"},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.selection.command("destroy", "--authorize "+authorizeDataLoss, "--yes")
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("remedy %q must carry %q; a refusal that drops the run's own selection names a command with a wider blast radius than the run the operator asked for", got, want)
				}
			}
			for _, deny := range tc.deny {
				if strings.Contains(got, deny) {
					t.Errorf("remedy %q must not invent %q the run never used", got, deny)
				}
			}
		})
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
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("safetyMatrixFlagExemptions exempts `bootwright %s --%s`, which no longer exists; a dead exemption hides the next flag that lands ungated", verb, name)
		}
	}
}
