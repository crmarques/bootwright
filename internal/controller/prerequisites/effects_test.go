package prerequisites

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Ansible resolves a module interpreter as one path and execs it. A quoted
// command line becomes a filename, and every module fails with rc 127.
func TestInterpreterScriptIsOneExecutableThatForwardsItsArguments(t *testing.T) {
	launch := PythonLaunch{
		Loader:    "/usr/lib64/ld-linux-x86-64.so.2",
		Arguments: []string{"--library-path", "/bundle/python/lib", "--glibc-hwcaps-mask", "", "/bundle/python/bin/python3.14"},
	}
	script := launch.InterpreterScript()
	if !strings.HasPrefix(script, "#!/bin/sh\n") || !strings.HasSuffix(script, " \"$@\"\n") {
		t.Fatalf("interpreter script = %q", script)
	}
	for _, want := range []string{"'/usr/lib64/ld-linux-x86-64.so.2'", "'--glibc-hwcaps-mask' ''", "'-I' '-B' '-S'"} {
		if !strings.Contains(script, want) {
			t.Fatalf("interpreter script = %q, missing %q", script, want)
		}
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "interpreter")
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the published interpreter is not a file: %v", err)
	}
}

func TestInterpreterScriptQuotesAnAdversarialArgument(t *testing.T) {
	launch := PythonLaunch{Loader: "/loader", Arguments: []string{"a'; rm -rf /; echo '"}}
	script := launch.InterpreterScript()
	if strings.Contains(script, "; rm -rf /;") && !strings.Contains(script, `'a'"'"'; rm -rf /; echo '"'"''`) {
		t.Fatalf("an argument escaped its quoting: %q", script)
	}
}
