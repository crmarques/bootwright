package machine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestARemoteCommandVectorNamesItsProgramsAbsolutely(t *testing.T) {
	for _, test := range []struct {
		name    string
		command RemoteCommand
		want    []string
	}{
		{"literal words stay the vector", RemoteCommand{Words: []string{"ls", "/var/lib/app/*"}}, []string{"ls", "/var/lib/app/*"}},
		{"sudo runs the literal words", RemoteCommand{Words: []string{"ls", "/x"}, Sudo: true}, []string{RemoteSudo, "-n", "--", "ls", "/x"}},
		{"a shell joins the words into one script", RemoteCommand{Words: []string{"ls", "/x/*", "|", "wc", "-l"}, Shell: true}, []string{RemoteShell, "-c", "--", "ls /x/* | wc -l"}},
		{"sudo runs the whole script", RemoteCommand{Words: []string{"ls /x/*"}, Shell: true, Sudo: true}, []string{RemoteSudo, "-n", "--", RemoteShell, "-c", "--", "ls /x/*"}},
		{"a script may start with a dash", RemoteCommand{Words: []string{"-n"}, Shell: true}, []string{RemoteShell, "-c", "--", "-n"}},
		{"bindings are exported before the script", RemoteCommand{Words: []string{`ls "$d"`}, Shell: true, Bindings: []ShellBinding{{"d", "/a b"}, {"e", ""}}},
			[]string{RemoteShell, "-c", "--", `export d='/a b' e=''; ls "$d"`}},
		{"no words stay no words under sudo", RemoteCommand{Sudo: true}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.command.Vector()
			if err != nil || !slices.Equal(got, test.want) {
				t.Fatalf("Vector() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestARemoteCommandRefusesWhatAShellCannotReadSafely(t *testing.T) {
	for _, test := range []struct {
		name    string
		command RemoteCommand
		want    string
	}{
		{"bindings without a shell", RemoteCommand{Words: []string{"ls"}, Bindings: []ShellBinding{{"a", "b"}}}, "--var requires --shell"},
		{"an empty script", RemoteCommand{Words: []string{" ", ""}, Shell: true}, "--shell needs a non-empty script"},
		{"no script", RemoteCommand{Shell: true}, "--shell needs a non-empty script"},
		{"a name with syntax", RemoteCommand{Words: []string{"ls"}, Shell: true, Bindings: []ShellBinding{{"a=$(id)", "b"}}}, "is not a shell variable name"},
		{"an empty name", RemoteCommand{Words: []string{"ls"}, Shell: true, Bindings: []ShellBinding{{"", "b"}}}, "is not a shell variable name"},
		{"a name set twice", RemoteCommand{Words: []string{"ls"}, Shell: true, Bindings: []ShellBinding{{"a", "b"}, {"a", "c"}}}, "is set twice"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got, err := test.command.Vector(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Vector() = %q, %v; want an error containing %q", got, err, test.want)
			}
		})
	}
}

// The login shell on the Machine reparses the quoted words the client joins.
// Running that same line through a real POSIX shell proves a binding arrives
// as data however hostile it is, while the script's own variables and globs
// take the target's values.
func TestABindingReachesTheScriptAsDataAndTheScriptExpandsTargetValues(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"one", "two"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hostile := `it's $(echo pwned) ` + "`echo pwned`" + ` "; echo pwned; '`
	vector, err := RemoteCommand{
		Words:    []string{`printf '[%s]\n' "$note" "$TARGET_ONLY" "$dir"/*`},
		Shell:    true,
		Bindings: []ShellBinding{{"note", hostile}, {"dir", dir}},
	}.Vector()
	if err != nil {
		t.Fatal(err)
	}
	got, err := asTheLoginShell(t, vector, "TARGET_ONLY=from the target")
	if err != nil {
		t.Fatalf("shell: %v", err)
	}
	want := "[" + hostile + "]\n[from the target]\n[" + filepath.Join(dir, "one") + "]\n[" + filepath.Join(dir, "two") + "]\n"
	if string(got) != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestALeadingAssignmentShapedWordRunsAsTheCommandItNames(t *testing.T) {
	got, err := asTheLoginShell(t, []string{"MARK=assigned", RemoteShell, "-c", `printf '%s' "${MARK-unset}"`})
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 127 || string(got) != "" {
		t.Fatalf("output = %q, err = %v; want the missing command MARK=assigned (127)", got, err)
	}
}

func asTheLoginShell(t *testing.T, vector []string, env ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	login := exec.CommandContext(ctx, RemoteShell, "-c", strings.Join(ShellLine(vector), " "))
	login.Env = append([]string{"PATH=/usr/bin:/bin"}, env...)
	return login.Output()
}
