package cli

import (
	"reflect"
	"strings"
	"testing"
)

func TestShellSudoAndVarsReachTheSessionAsOneVector(t *testing.T) {
	for _, path := range []string{"machine exec", "cluster exec"} {
		t.Run(path, func(t *testing.T) {
			args := append(strings.Fields(path), "--name", "demo", "--shell", "--sudo",
				"--var", "dir=/var/lib/app", "--var", "note=it's $(id)", "--", `ls "$dir"/*`, "&&", `echo "$note"`)
			code, _, errOut, record := runRecorded(args)
			if record.calls != 1 {
				t.Fatalf("code=%d stderr=%q calls=%d", code, errOut, record.calls)
			}
			want := []string{"/usr/bin/sudo", "-n", "--", "/bin/sh", "-c", "--", `export dir=/var/lib/app note='it'\''s $(id)'; ls "$dir"/* && echo "$note"`}
			if got := reflect.ValueOf(record.request).FieldByName("Command").Interface().([]string); !reflect.DeepEqual(got, want) {
				t.Fatalf("command = %q, want %q", got, want)
			}
		})
	}
}

func TestSudoAloneRunsTheExactWordsThroughTheMachinesSudo(t *testing.T) {
	_, _, errOut, record := runRecorded([]string{"machine", "exec", "--name", "demo", "--sudo", "--", "ls", "/var/lib/app/*"})
	if record.calls != 1 {
		t.Fatalf("stderr=%q calls=%d", errOut, record.calls)
	}
	want := []string{"/usr/bin/sudo", "-n", "--", "ls", "/var/lib/app/*"}
	if got := reflect.ValueOf(record.request).FieldByName("Command").Interface().([]string); !reflect.DeepEqual(got, want) {
		t.Fatalf("command = %q, want %q", got, want)
	}
}

func TestARemoteCommandMistakeIsAUsageRefusalThatNeverEchoesAValue(t *testing.T) {
	const private = "do-not-display-private-value"
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"var without shell", []string{"--var", "a=" + private, "--", "ls"}, "--var requires --shell: only a script reads variables"},
		{"var without a value", []string{"--shell", "--var", private, "--", "ls"}, "a --var entry has no =: write --var <name>=<value>"},
		{"var name not a shell name", []string{"--shell", "--var", "1dir=" + private, "--", "ls"}, `--var name "1dir" is not a shell variable name`},
		{"var name with shell syntax", []string{"--shell", "--var", "a;id=" + private, "--", "ls"}, `--var name "a;id" is not a shell variable name`},
		{"var set twice", []string{"--shell", "--var", "a=" + private, "--var", "a=b", "--", "ls"}, `--var name "a" is set twice`},
		{"empty script", []string{"--shell", "--", " "}, "--shell needs a non-empty script"},
	} {
		for _, path := range []string{"machine exec", "cluster exec"} {
			t.Run(path+"/"+test.name, func(t *testing.T) {
				args := append(append(strings.Fields(path), "--name", "demo"), test.args...)
				code, out, errOut, record := runRecorded(args)
				if code != 2 || out != "" || record.calls != 0 || !strings.HasPrefix(errOut, "  error  cli.usage\n    "+test.want) {
					t.Fatalf("code=%d out=%q stderr=%q calls=%d", code, out, errOut, record.calls)
				}
				if strings.Contains(errOut, private) {
					t.Fatalf("diagnostic exposed a value: %q", errOut)
				}
			})
		}
	}
}
