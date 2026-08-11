package cli

import (
	"bytes"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

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
			if !exercised[verb][f.Name] {
				missing = append(missing, f.Name)
			}
		}
		cmd.Flags().VisitAll(visit)
		cmd.InheritedFlags().VisitAll(visit)
		sort.Strings(missing)
		for _, name := range missing {
			t.Errorf("`bootwright %s --%s` is registered but no safetyMatrixCases() row passes it; every flag on a mutating verb must have a matrix row asserting its verdict", verb, name)
		}
	}
}
