package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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

var scopedFlags = []string{"context", "ssh-id-file", "ssh-user", "ssh-ask-sudo-password", "ssh-user-for-provisioned", "ssh-ask-password"}

func scopedFlagArguments(name string) []string {
	if name == "ssh-ask-sudo-password" || name == "ssh-user-for-provisioned" || name == "ssh-ask-password" {
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
			if err := flags.Set("dry-run", "false"); err != nil {
				t.Fatal(err)
			}
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

type matrixRow struct {
	commands []string
	accepts  map[string]bool
}

func inheritedFlagMatrix(t *testing.T) (rows []matrixRow, fallback matrixRow) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "specs", "cli", "commands.md"))
	if err != nil {
		t.Fatal(err)
	}
	var header []string
	for _, line := range strings.Split(string(data), "\n") {
		cells := strings.Split(strings.Trim(line, "| "), " | ")
		switch {
		case strings.HasPrefix(line, "| Commands | `--context`"):
			header = nil
			for _, cell := range cells[1:] {
				header = append(header, strings.TrimPrefix(strings.Trim(cell, "`"), "--"))
			}
		case header != nil && strings.HasPrefix(line, "| ---"):
		case header != nil && strings.HasPrefix(line, "| ") && len(cells) == len(header)+1:
			row := matrixRow{accepts: map[string]bool{}}
			for index, name := range header {
				row.accepts[name] = cells[index+1] == "yes"
			}
			if strings.HasPrefix(cells[0], "every other") {
				fallback = row
				continue
			}
			for _, match := range backticked.FindAllStringSubmatch(cells[0], -1) {
				row.commands = append(row.commands, match[1])
			}
			rows = append(rows, row)
		default:
			header = nil
		}
	}
	if len(rows) < 5 || fallback.accepts == nil {
		t.Fatalf("read %d matrix rows; the matrix of specs/cli/commands.md has changed shape", len(rows))
	}
	return rows, fallback
}

func TestTheInheritedFlagMatrixIsTheFlagsEachCommandReads(t *testing.T) {
	rows, fallback := inheritedFlagMatrix(t)
	for _, spec := range commandCatalog() {
		row := fallback
		for _, candidate := range rows {
			for _, command := range candidate.commands {
				if spec.path == command || strings.HasPrefix(spec.path, command+" ") {
					row = candidate
				}
			}
		}
		contextFree, sshFlags := inheritedFlagScope(spec.path)
		if row.accepts["context"] == contextFree {
			t.Errorf("%s accepts --context: %t in the matrix, but is context-free: %t", spec.path, row.accepts["context"], contextFree)
		}
		for _, name := range scopedFlags[1:] {
			if row.accepts[name] != slices.Contains(sshFlags, name) {
				t.Errorf("%s accepts --%s: %t in the matrix, but its SSH flags are %v", spec.path, name, row.accepts[name], sshFlags)
			}
		}
	}
}
