package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/nativeartifacts/comparisonlocal"
	"github.com/crmarques/bootwright/internal/nativeartifacts/renderlocal"
)

func serveComparison(t *testing.T, directory string) (int, string) {
	t.Helper()
	request, err := json.Marshal(map[string]any{
		"uid": os.Getuid(), "gid": os.Getegid(), "action": "publish", "directory": directory,
		"files": []map[string]any{{"Path": "desired/pool.yaml", "Data": []byte("kind: Pool\n")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	handled, code := comparisonlocal.ServeHelper(context.Background(), []string{comparisonlocal.HelperMode}, bytes.NewReader(request), &out, renderlocal.Files{})
	if !handled {
		t.Fatal("the helper mode was not handled")
	}
	return code, out.String()
}

func TestTheComparisonHelperWritesAPrivateTreeAsItsAccount(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("the helper refuses to serve root")
	}
	directory := filepath.Join(t.TempDir(), "cmp")
	if code, out := serveComparison(t, directory); code != 0 || strings.Contains(out, `"failed":true`) {
		t.Fatalf("exit %d, response %s", code, out)
	}
	for name, mode := range map[string]os.FileMode{"": 0o700, "desired": 0o700, "desired/pool.yaml": 0o600} {
		if info, err := os.Stat(filepath.Join(directory, name)); err != nil || info.Mode().Perm() != mode {
			t.Errorf("%q: %v, %v, want %v", name, info, err, mode)
		}
	}
}

func TestTheComparisonHelperRefusesANonemptyDirectoryAndLeavesItAlone(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("the helper refuses to serve root")
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "mine"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out := serveComparison(t, directory)
	if code != 0 || !strings.Contains(out, `"failed":true`) || !strings.Contains(out, "is not empty") {
		t.Fatalf("exit %d, response %s", code, out)
	}
	if entries, err := os.ReadDir(directory); err != nil || len(entries) != 1 || entries[0].Name() != "mine" {
		t.Fatalf("entries %v, %v", entries, err)
	}
}
