package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWrapDisplayTextNeverSplitsAToken(t *testing.T) {
	token := strings.Repeat("x", 40)
	got := wrapDisplayText("a "+token, 10)
	if want := []string{"a", token}; !reflect.DeepEqual(got, want) {
		t.Fatalf("wrapDisplayText = %q, want %q", got, want)
	}
	got = wrapDisplayText("a "+token+" b c", 10)
	if want := []string{"a", token, "b c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("wrapDisplayText = %q, want %q", got, want)
	}
}

func TestWrapDisplayTextNeverOpensALineWithASpace(t *testing.T) {
	for _, test := range []struct {
		value string
		width int
		want  []string
	}{
		{strings.Repeat("A", 20) + "  (note here)", 10, []string{strings.Repeat("A", 20), "(note", "here)"}},
		{"aaa bbb  ccc", 7, []string{"aaa bbb", "ccc"}},
		{" lead", 10, []string{" lead"}},
	} {
		if got := wrapDisplayText(test.value, test.width); !reflect.DeepEqual(got, test.want) {
			t.Errorf("wrapDisplayText(%q, %d) = %q, want %q", test.value, test.width, got, test.want)
		}
	}
}

func TestResourceHeadingNeverSplitsTheName(t *testing.T) {
	name := "rhel-" + strings.Repeat("a", 50)
	for _, state := range []string{"applied", ""} {
		var text display
		text.resourceHeading("machine", name, state)
		var out bytes.Buffer
		if err := text.writeTo(WithPresentation(&out, PresentationOptions{Width: 40, Color: "never"})); err != nil {
			t.Fatal(err)
		}
		want := "  ○ " + name + "\n"
		if state != "" {
			want += "    " + state + "\n"
		}
		if out.String() != want {
			t.Fatalf("heading with state %q = %q, want %q", state, out.String(), want)
		}
	}
}

func TestOverlongTokensKeepTheirFullTextOnOneLine(t *testing.T) {
	path := "/var/lib/example/contexts/lab/state/operations/op-6f1c2a9e0b7d4c3f8a5e2d1b0c9f8e7a/logs"
	command := "ceph --conf /etc/ceph/ceph.conf --keyring /etc/ceph/ceph.client.admin.keyring status"
	var text display
	text.verbatimFields(field{Label: "Logs", Value: path}, field{Label: "Next", Value: command})
	text.fields(field{Label: "Other", Value: path})
	text.mutedVerbatimFields(field{Label: "Ceph command", Value: command})
	text.mutedVerbatimLines([]string{command})
	text.lines([]string{path})
	var out bytes.Buffer
	if err := text.writeTo(WithPresentation(&out, PresentationOptions{Width: 40, Color: "never"})); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	count := func(want string) int {
		n := 0
		for _, line := range lines {
			if strings.Contains(line, want) {
				n++
			}
		}
		return n
	}
	if got := count(path); got != 3 {
		t.Fatalf("path appears whole on %d lines, want 3 (verbatim field, wrapped field, line): %q", got, out.String())
	}
	if got := count(command); got != 3 {
		t.Fatalf("command appears whole on %d lines, want 3: %q", got, out.String())
	}
	for _, line := range lines {
		if strings.Contains(line, "...") {
			t.Fatalf("a line was truncated: %q", line)
		}
	}
}

func TestHumanGoldensKeepPathsAndCommandsOnOneLine(t *testing.T) {
	for _, test := range []struct{ golden, want string }{
		{"cli-apply-failed", "Logs  /var/lib/bootwright/contexts/lab/state/operations/op-6f1c2a9e0b7d4c3f8a5e2d1b0c9f8e7a/logs"},
		{"cli-status", "/var/lib/bootwright/contexts/lab/state/operations/op-6f1c2a9e0b7d4c3f8a5e2d1b0c9f8e7a/logs"},
		{"cli-cluster-info", "Ceph command  bootwright cluster exec --context lab --name storage --node rhel-01 -- sudo -n ceph --conf " +
			"/var/lib/bootwright/ceph/6b8f1c2e-4d7a-5e3f-9a1b-0c2d4e6f8a1b/ceph.conf --keyring " +
			"/var/lib/bootwright/ceph/6b8f1c2e-4d7a-5e3f-9a1b-0c2d4e6f8a1b/ceph.client.admin.keyring"},
	} {
		data, err := os.ReadFile(filepath.Join("testdata", test.golden+".golden"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), test.want+"\n") {
			t.Errorf("%s: %q is not whole on one line", test.golden, test.want)
		}
	}
}
