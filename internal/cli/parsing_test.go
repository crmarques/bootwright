package cli

import (
	"strings"
	"testing"
)

func TestRawArgumentBounds(t *testing.T) {
	for _, count := range []int{maxArguments, maxArguments + 1} {
		args := make([]string, count)
		if got := rawBounds(args); (got == "") != (count == maxArguments) {
			t.Errorf("count=%d: %q", count, got)
		}
	}
	for _, size := range []int{maxArgumentBytes, maxArgumentBytes + 1} {
		if got := rawBounds([]string{strings.Repeat("x", size)}); (got == "") != (size == maxArgumentBytes) {
			t.Errorf("size=%d: %q", size, got)
		}
	}
	args := make([]string, maxTotalArgumentBytes/maxArgumentBytes)
	for i := range args {
		args[i] = strings.Repeat("x", maxArgumentBytes)
	}
	if message := rawBounds(args); message != "" {
		t.Fatal(message)
	}
	args = append(args, "x")
	if message := rawBounds(args); !strings.Contains(message, "total") {
		t.Fatalf("total boundary: %q", message)
	}
	args = make([]string, maxArguments+1)
	args[0] = strings.Repeat("x", maxArgumentBytes+1)
	if message := rawBounds(args); !strings.Contains(message, "count") {
		t.Fatalf("count precedence: %q", message)
	}
	args = []string{strings.Repeat("é", maxArgumentBytes/2+1)}
	if message := rawBounds(args); message == "" {
		t.Fatal("bounds counted runes instead of bytes")
	}
}

func TestRawBoundsPrecedeHelpAndJSON(t *testing.T) {
	for _, prefix := range [][]string{{"--help"}, {"machine", "list", "--output", "json"}, {"__bootwright_complete"}} {
		args := append(append([]string(nil), prefix...), strings.Repeat("do-not-display", 2000))
		code, out, errOut, record := runRecorded(args)
		if code != 2 || out != "" || record.calls != 0 || !strings.Contains(errOut, "cli.usage") || !strings.Contains(errOut, "Usage: bootwright") || strings.Contains(errOut, "do-not-display") {
			t.Fatalf("code=%d out=%q stderr=%q calls=%d", code, out, errOut, record.calls)
		}
	}
}

func TestUsageJSONRespectsLocalFlagOwnership(t *testing.T) {
	for _, test := range []struct {
		args []string
		json bool
	}{
		{[]string{"render", "--output", "json", "effective"}, false},
		{[]string{"render", "--output=json", "effective", "--help"}, false},
		{[]string{"render", "--output", "json", "effective", "--unknown"}, false},
		{[]string{"render", "--output", "json", "effective", "--output", "text"}, false},
		{[]string{"render", "--output", "text", "effective", "--output", "json"}, true},
		{[]string{"render", "--clusters", "--output=json", "effective"}, false},
	} {
		code, out, errOut, record := runRecorded(test.args)
		if code != 2 || record.calls != 0 {
			t.Fatalf("%q: code=%d calls=%d", test.args, code, record.calls)
		}
		if test.json {
			if !strings.Contains(out, `"command":"render effective"`) || !strings.Contains(out, `"code":"cli.usage"`) || errOut != "" {
				t.Fatalf("%q: out=%q stderr=%q", test.args, out, errOut)
			}
		} else if out != "" || !strings.Contains(errOut, "cli.usage") {
			t.Fatalf("%q: out=%q stderr=%q", test.args, out, errOut)
		}
	}
}

func TestRenderOperandsFollowHelpPrecedence(t *testing.T) {
	for _, args := range [][]string{
		{"render", "extra", "--help"},
		{"render", "extra", "effective", "--help"},
		{"render", "--input-dir", "inputs", "--output-dir", "artifacts", "extra", "--help"},
	} {
		code, out, errOut, record := runRecorded(args)
		if code != 0 || errOut != "" || record.calls != 0 || !strings.Contains(out, "Context-free rendering") {
			t.Fatalf("%q: code=%d out=%q stderr=%q calls=%d", args, code, out, errOut, record.calls)
		}
	}
	for _, args := range [][]string{{"render", "extra"}, {"render", "extra", "effective"}, {"help", "render", "extra"}, {"context", "extra", "--help"}} {
		code, out, errOut, record := runRecorded(args)
		if code != 2 || out != "" || record.calls != 0 || !strings.Contains(errOut, "cli.usage") {
			t.Fatalf("%q: code=%d out=%q stderr=%q calls=%d", args, code, out, errOut, record.calls)
		}
	}
}

func FuzzRawBounds(f *testing.F) {
	f.Add("context", "--help")
	f.Add(strings.Repeat("x", maxArgumentBytes), "")
	f.Fuzz(func(t *testing.T, first, second string) {
		args := []string{first, second}
		valid := len(first) <= maxArgumentBytes && len(second) <= maxArgumentBytes && len(first)+len(second) <= maxTotalArgumentBytes
		if (rawBounds(args) == "") != valid {
			t.Fatal("raw bounds disagree with byte limits")
		}
	})
}

func FuzzInvocation(f *testing.F) {
	for _, args := range [][]string{{"machine", "list", "--silent", "--output", "json"}, {"cluster", "oc", "--name", "demo", "--", "--help"}, {"help", "render"}, {"__bootwright_complete", "cluster", ""}, {"context", "init", "--name=", "--help"}} {
		f.Add(strings.Join(args, "\x00"))
	}
	f.Fuzz(func(t *testing.T, encoded string) {
		if len(encoded) > maxTotalArgumentBytes+maxArguments {
			t.Skip()
		}
		code, _, _, record := runRecorded(strings.Split(encoded, "\x00"))
		if code < 0 || code > 2 || record.calls > 1 {
			t.Fatalf("code=%d calls=%d", code, record.calls)
		}
	})
}
