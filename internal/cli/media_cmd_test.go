package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/infra/media"
)

func TestMediaCLIAddListRemove(t *testing.T) {
	setTestHomeAndRoot(t)
	source := filepath.Join(t.TempDir(), "rhel.iso")
	if err := os.WriteFile(source, []byte("iso bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, "media", "add", "--name", "rhel.iso", "--from-file", source)
	if code != 0 {
		t.Fatalf("media add exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "stored at") {
		t.Fatalf("media add output = %q", stdout)
	}

	stdout, stderr, code = runCLI(t, "media", "list")
	if code != 0 {
		t.Fatalf("media list exited %d, stderr=%q", code, stderr)
	}
	if strings.Contains(stdout, "sha256:") {
		t.Fatalf("default media list calculated a checksum: %q", stdout)
	}

	stdout, stderr, code = runCLI(t, "media", "list", "--output", "json")
	if code != 0 {
		t.Fatalf("media list exited %d, stderr=%q", code, stderr)
	}
	if strings.Contains(stdout, `"sha256"`) {
		t.Fatalf("default media list JSON included a checksum: %q", stdout)
	}
	var report mediaListReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode media list: %v\n%s", err, stdout)
	}
	if len(report.Media) != 1 || report.Media[0].Name != "rhel.iso" || report.Media[0].Reference != "local-media:rhel.iso" {
		t.Fatalf("report = %#v", report)
	}

	wantSum := sha256.Sum256([]byte("iso bytes"))
	wantSHA256 := hex.EncodeToString(wantSum[:])
	stdout, stderr, code = runCLI(t, "media", "list", "--checksums")
	if code != 0 {
		t.Fatalf("media list --checksums exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "sha256:"+wantSHA256) {
		t.Fatalf("media list --checksums omitted the checksum: %q", stdout)
	}

	stdout, stderr, code = runCLI(t, "media", "list", "--checksums", "--output", "json")
	if code != 0 {
		t.Fatalf("media list --checksums --output json exited %d, stderr=%q", code, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode checked media list: %v\n%s", err, stdout)
	}
	if len(report.Media) != 1 || report.Media[0].SHA256 != wantSHA256 {
		t.Fatalf("checked report = %#v", report)
	}

	stdout, stderr, code = runCLI(t, "media", "delete", "--name", "rhel.iso", "--yes")
	if code != 0 {
		t.Fatalf("media delete exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(media.StoreDir(), "rhel.iso")); !os.IsNotExist(err) {
		t.Fatalf("stored ISO still exists or stat failed: %v", err)
	}
}

func TestMediaListChecksumsHelpNamesFullRead(t *testing.T) {
	stdout, stderr, code := runCLI(t, "media", "list", "--help")
	if code != 0 {
		t.Fatalf("media list --help exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{"--checksums", "reads every ISO in full"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("media list --help missing %q:\n%s", want, stdout)
		}
	}
}

func TestMediaCLIRejectsInvalidAndDuplicate(t *testing.T) {
	setTestHomeAndRoot(t)
	source := filepath.Join(t.TempDir(), "rhel.iso")
	if err := os.WriteFile(source, []byte("iso bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLI(t, "media", "add", "--name", "../rhel.iso", "--from-file", source)
	if code == 0 || !strings.Contains(stderr, "must be a filename") {
		t.Fatalf("invalid key code=%d stderr=%q", code, stderr)
	}
	if _, stderr, code = runCLI(t, "media", "add", "--name", "rhel.iso", "--from-file", source); code != 0 {
		t.Fatalf("initial add failed: %q", stderr)
	}
	replacement := filepath.Join(t.TempDir(), "replacement.iso")
	if err := os.WriteFile(replacement, []byte("replacement bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runCLI(t, "media", "add", "--name", "rhel.iso", "--from-file", replacement)
	if code == 0 {
		t.Fatal("duplicate media add without --yes unexpectedly succeeded")
	}
	if !strings.Contains(stdout, "Replace media rhel.iso") {
		t.Fatalf("stdout missing media replacement prompt: %q", stdout)
	}
	if !strings.Contains(stderr, "media add aborted") {
		t.Fatalf("stderr missing media add abort: %q", stderr)
	}
	stored, err := os.ReadFile(filepath.Join(media.StoreDir(), "rhel.iso"))
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != "iso bytes" {
		t.Fatalf("aborted media replacement changed stored bytes: %q", stored)
	}
	stdout, stderr, code = runCLIWithInput(t, "y\n", "media", "add", "--name", "rhel.iso", "--from-file", replacement)
	if code != 0 {
		t.Fatalf("confirmed media replacement exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	stored, err = os.ReadFile(filepath.Join(media.StoreDir(), "rhel.iso"))
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != "replacement bytes" {
		t.Fatalf("confirmed media replacement stored bytes = %q", stored)
	}
}
