//go:build completion

package completion

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGeneratedIntegrations(t *testing.T) {
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "bootwright")
	build := exec.Command(filepath.Join(repository, "scripts/go"), "build", "-buildvcs=false", "-o", binary, "./cmd/bootwright")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build executable: %v\n%s", err, output)
	}
	if err := os.WriteFile(filepath.Join(workspace, "forbidden-filesystem-candidate"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	// Path candidates come from the executable, never from shell expansion, so
	// these fixtures prove the executable enumerated them. The shared prefix
	// keeps each expectation independent of everything else in the workspace.
	if err := os.MkdirAll(filepath.Join(workspace, "fixture-alpha", "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, "fixture-beta"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "fixture-alpha", "input.yaml"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	// Bash is the qualified shell for this delivery. The other generated
	// integrations still ship and are still exercised by the same cases; select
	// them with BOOTWRIGHT_TEST_ALL_SHELLS=1 on a runner that provides them.
	shells := []struct{ name, executable, variable string }{
		{"bash", "bash", "BOOTWRIGHT_TEST_BASH"},
	}
	if os.Getenv("BOOTWRIGHT_TEST_ALL_SHELLS") == "1" {
		shells = append(shells,
			struct{ name, executable, variable string }{"zsh", "zsh", "BOOTWRIGHT_TEST_ZSH"},
			struct{ name, executable, variable string }{"fish", "fish", "BOOTWRIGHT_TEST_FISH"},
			struct{ name, executable, variable string }{"powershell", "pwsh", "BOOTWRIGHT_TEST_POWERSHELL"},
		)
	}
	for _, shell := range shells {
		t.Run(shell.name, func(t *testing.T) {
			runtime := os.Getenv(shell.variable)
			if runtime == "" {
				runtime, err = exec.LookPath(shell.executable)
				if err != nil {
					t.Fatalf("required runtime unavailable: set %s or install %s: %v", shell.variable, shell.executable, err)
				}
			}
			for _, noDescriptions := range []bool{false, true} {
				t.Run(fmt.Sprintf("no-descriptions=%t", noDescriptions), func(t *testing.T) {
					arguments := []string{"completion", shell.name}
					if noDescriptions {
						arguments = append(arguments, "--no-descriptions")
					}
					generator := exec.Command(binary, arguments...)
					script, err := generator.Output()
					if err != nil {
						t.Fatalf("generate script: %v", err)
					}
					extension := ".completion"
					if shell.name == "powershell" {
						extension = ".ps1"
					}
					path := filepath.Join(workspace, shell.name+extension)
					if err := os.WriteFile(path, script, 0600); err != nil {
						t.Fatal(err)
					}
					for _, test := range []struct {
						name  string
						words []string
						want  []string
					}{
						{"commands", []string{"c"}, []string{"cluster", "completion", "context"}},
						{"nested commands", []string{"cluster", "k"}, []string{"kubeconfig", "kubectl"}},
						{"flags", []string{"validate", "--o"}, []string{"--output"}},
						{"enum", []string{"validate", "--output", ""}, []string{"json", "text"}},
						{"enum prefix", []string{"validate", "--output", "j"}, []string{"json"}},
						{"attached enum", []string{"validate", "--output=j"}, []string{"--output=json"}},
						{"no removed backend flag", []string{"secret", "encryption", "init", "--type", ""}, nil},
						{"no attached removed backend flag", []string{"secret", "encryption", "init", "--type=loc"}, nil},
						{"no withdrawn watch flag", []string{"status", "--w"}, nil},
						{"no withdrawn verbose flag", []string{"apply", "--v"}, nil},
						{"input directory flag", []string{"context", "init", "--input"}, []string{"--input-dir"}},
						{"directory paths", []string{"context", "init", "--input-dir", "fixture"}, []string{"fixture-alpha/", "fixture-beta/"}},
						{"directory paths exclude files", []string{"context", "init", "--input-dir", "fixture-alpha/"}, []string{"fixture-alpha/nested/"}},
						{"file paths include directories", []string{"validate", "--file", "fixture-alpha/"}, []string{"fixture-alpha/input.yaml", "fixture-alpha/nested/"}},
						{"no paths for value flags", []string{"context", "use", "--name", "forbidden"}, nil},
						{"no names", []string{"context", "use", "--name", ""}, nil},
						{"no hidden handlers", []string{"__"}, nil},
						{"no removed roots", []string{"example", ""}, nil},
						{"no payload completion", []string{"machine", "exec", "--name", "sample", "--", ""}, nil},
					} {
						t.Run(test.name, func(t *testing.T) {
							ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
							defer cancel()
							command := shellQuery(t, ctx, shell.name, runtime, path, test.words)
							command.Dir = workspace
							command.Env = append(os.Environ(), "PATH="+workspace+string(os.PathListSeparator)+os.Getenv("PATH"))
							output, err := command.CombinedOutput()
							if err != nil {
								t.Fatalf("query completion: %v\n%s", err, output)
							}
							var candidates []string
							for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
								candidate, _, _ := strings.Cut(line, "\t")
								if candidate != "" {
									candidates = append(candidates, candidate)
								}
							}
							if !reflect.DeepEqual(candidates, test.want) {
								t.Fatalf("candidates = %q; want %q; output = %q", candidates, test.want, output)
							}
							if noDescriptions && strings.Contains(string(output), "\t") {
								t.Fatalf("description leaked: %q", output)
							}
							if !noDescriptions && shell.name != "bash" && test.name == "commands" && !strings.Contains(string(output), "\t") {
								t.Fatalf("shell description channel was not populated: %q", output)
							}
						})
					}
				})
			}
		})
	}
}

func shellQuery(t *testing.T, ctx context.Context, shell, runtime, script string, words []string) *exec.Cmd {
	t.Helper()
	line := "bootwright " + strings.Join(words, " ")
	switch shell {
	case "bash":
		program := `source "$1"
shift
COMP_WORDS=(bootwright "$@")
COMP_CWORD=$((${#COMP_WORDS[@]}-1))
_bootwright_complete
if ((${#COMPREPLY[@]})); then printf '%s\n' "${COMPREPLY[@]}"; fi`
		return exec.CommandContext(ctx, runtime, append([]string{"--noprofile", "--norc", "-c", program, "completion-test", script}, words...)...)
	case "zsh":
		program := `compdef() { :; }
compadd() {
    while (( $# )) && [[ $1 != -- ]]; do shift; done
    shift
    local index=1 candidate
    for candidate in "$@"; do
        if [[ ${descriptions[index]} != $candidate ]]; then
            printf '%s\t%s\n' "$candidate" "${descriptions[index]}"
        else
            printf '%s\n' "$candidate"
        fi
        ((index++))
    done
}
source "$1"
shift
words=(bootwright "$@")
CURRENT=${#words}
_bootwright_complete`
		return exec.CommandContext(ctx, runtime, append([]string{"-f", "-c", program, "completion-test", script}, words...)...)
	case "fish":
		return exec.CommandContext(ctx, runtime, "--no-config", "-c", `source $argv[1]; complete --do-complete=$argv[2]`, script, line)
	default:
		program := `param($Script, $Line)
. $Script
$result = [System.Management.Automation.CommandCompletion]::CompleteInput($Line, $Line.Length, $null)
foreach ($match in $result.CompletionMatches) {
    if ($match.ToolTip -ne $match.CompletionText) { $match.CompletionText + [char]9 + $match.ToolTip }
    else { $match.CompletionText }
}`
		probe := script + ".probe.ps1"
		if err := os.WriteFile(probe, []byte(program), 0600); err != nil {
			t.Fatal(err)
		}
		return exec.CommandContext(ctx, runtime, "-NoLogo", "-NoProfile", "-NonInteractive", "-File", probe, script, line)
	}
}
