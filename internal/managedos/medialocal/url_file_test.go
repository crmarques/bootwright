package medialocal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

const signedURL = "https://mirror.example.test/rhel.iso?X-Amz-Signature=secret"

func stdinOf(content string) func(context.Context, []byte) (int, error) {
	remaining := []byte(content)
	return func(_ context.Context, buffer []byte) (int, error) {
		n := copy(buffer, remaining)
		remaining = remaining[n:]
		return n, nil
	}
}

// A signed URL is read from a file opened through the invoking account's
// opener, or from standard input, as exactly one line, and no refusal names
// what it read.
func TestAURLFileHoldsExactlyOneURLLine(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "url"), []byte(signedURL+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	acquirer := New(nil, virtualFiles{root: root})
	if got, err := acquirer.ReadURL(context.Background(), "/url"); err != nil || got != signedURL {
		t.Fatalf("file URL = %q (%v)", got, err)
	}
	if got, err := acquirer.Reading(stdinOf(signedURL)).ReadURL(context.Background(), "-"); err != nil || got != signedURL {
		t.Fatalf("standard-input URL = %q (%v)", got, err)
	}
	for name, content := range map[string]string{
		"empty":     "",
		"two lines": signedURL + "\n" + signedURL + "\n",
		"carriage":  signedURL + "\r\n",
		"oversize":  "https://mirror.example.test/" + strings.Repeat("a", MaxURLFileBytes),
	} {
		_, err := acquirer.Reading(stdinOf(content)).ReadURL(context.Background(), "-")
		expectRefusal(t, err)
		if reported := diagnostics.Of(err); strings.Contains(reported[0].Message+reported[0].Remediation, "secret") {
			t.Fatalf("%s: the refusal names what it read: %+v", name, reported[0])
		}
	}
	if _, err := New(nil, virtualFiles{root: root}).ReadURL(context.Background(), "-"); err == nil {
		t.Fatal("standard input was read with no reader configured")
	}
	if _, err := New(nil, virtualFiles{root: root}).ReadURL(context.Background(), "/absent"); err == nil {
		t.Fatal("an absent URL file was read")
	}
}
