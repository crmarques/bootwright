package cli

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

type readRecorder struct {
	pflag.Value
	name  string
	reads map[string]bool
}

func (r *readRecorder) String() string {
	r.reads[r.name] = true
	return r.Value.String()
}

var scopedFlags = []string{"context", "ssh-id-file", "ssh-user", "ssh-ask-sudo-password", "ssh-user-for-provisioned"}

func scopedFlagArguments(name string) []string {
	if name == "ssh-ask-sudo-password" || name == "ssh-user-for-provisioned" {
		return []string{"--" + name}
	}
	return []string{"--" + name, "value"}
}

func flagsReadBy(t *testing.T, path string) map[string]bool {
	t.Helper()
	reads := map[string]bool{}
	for _, withFiles := range []bool{true, false} {
		flags := dispatchFlags()
		if !withFiles {
			if err := flags.Set("file", ""); err != nil {
				t.Fatal(err)
			}
		}
		flags.VisitAll(func(flag *pflag.Flag) { flag.Value = &readRecorder{Value: flag.Value, name: flag.Name, reads: reads} })
		_, _ = dispatchSpies(&dispatchRecord{}).invoke(context.Background(), path, flags, []string{"operand"})
	}
	return reads
}

func TestInheritedFlagScopeIsTheFlagsEachCommandReads(t *testing.T) {
	for _, spec := range commandCatalog() {
		t.Run(spec.path, func(t *testing.T) {
			reads := flagsReadBy(t, spec.path)
			contextFree, sshFlags := inheritedFlagScope(spec.path)
			if reads["context"] == contextFree {
				t.Errorf("%s reads --context: %t, but is marked context-free: %t", spec.path, reads["context"], contextFree)
			}
			for _, name := range scopedFlags[1:] {
				if want := slices.Contains(sshFlags, name); reads[name] != want {
					t.Errorf("%s reads --%s: %t, but its SSH flags %v list it: %t", spec.path, name, reads[name], sshFlags, want)
				}
			}
		})
	}
	for path := range contextFreeCommands {
		if specFor(path).path != path {
			t.Errorf("contextFreeCommands names %q, which the catalog does not list", path)
		}
	}
	for path := range sshFlagsByCommand {
		if specFor(path).path != path {
			t.Errorf("sshFlagsByCommand names %q, which the catalog does not list", path)
		}
	}
}

func TestACommandRefusesAnInheritedFlagItDoesNotRead(t *testing.T) {
	for _, spec := range commandCatalog() {
		contextFree, sshFlags := inheritedFlagScope(spec.path)
		for _, name := range scopedFlags {
			free := contextFree
			if name != "context" {
				free = !slices.Contains(sshFlags, name)
			}
			after := append(strings.Fields(spec.path), scopedFlagArguments(name)...)
			before := append(scopedFlagArguments(name), strings.Fields(spec.path)...)
			for _, args := range [][]string{after, before} {
				t.Run(strings.Join(args, " "), func(t *testing.T) {
					_, out, errOut, record := runRecorded(args)
					refused := strings.Contains(out+errOut, "flag is not accepted by this command")
					if refused != free || free && record.calls != 0 {
						t.Fatalf("refused=%t, want %t; calls=%d out=%q err=%q", refused, free, record.calls, out, errOut)
					}
				})
			}
		}
	}
}

func TestHelpAndCompletionListExactlyTheInheritedFlagsACommandReads(t *testing.T) {
	root, err := newCommandTree(New(Config{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range commandCatalog() {
		if spec.path == "help" {
			continue
		}
		reads := flagsReadBy(t, spec.path)
		for _, format := range []string{"text", "json"} {
			var out, errOut bytes.Buffer
			args := append(strings.Fields(spec.path), "--help", "--detail", "full", "--output", format)
			if code := New(Config{Out: &out, ErrOut: &errOut}).Run(context.Background(), args); code != 0 {
				t.Fatalf("%s help: %d %q", spec.path, code, errOut.String())
			}
			for _, name := range scopedFlags {
				listed := strings.Contains(out.String(), "--"+name+" ") || strings.Contains(out.String(), `"name":"`+name+`"`)
				if listed != reads[name] {
					t.Errorf("%s %s help lists --%s: %t, but the command reads it: %t", spec.path, format, name, listed, reads[name])
				}
			}
		}
		candidates := completionCandidatesWithCatalog(root, append(strings.Fields(spec.path), "--"), false, completionCatalog{})
		for _, name := range scopedFlags {
			if slices.Contains(candidates, "--"+name) != reads[name] {
				t.Errorf("%s completion offers --%s: %t, but the command reads it: %t", spec.path, name, !reads[name], reads[name])
			}
		}
	}
}
