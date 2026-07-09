package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedInput populates a context's input directory with one file so there is a
// tree to snapshot.
func seedInput(t *testing.T, ctx Context, rel, contents string) {
	t.Helper()
	writeFile(t, filepath.Join(ctx.InputDir, filepath.FromSlash(rel)), contents)
}

func newTestContextWithInput(t *testing.T) Context {
	t.Helper()
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	ctx, err := NewContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureDirs(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestSnapshotInputCopiesTreeAndRecordsMeta(t *testing.T) {
	ctx := newTestContextWithInput(t)
	seedInput(t, ctx, "clusters/storage/ceph/cluster.yaml", "kind: StorageCluster\n")
	seedInput(t, ctx, "environment.yaml", "kind: Environment\n")

	entry, err := SnapshotInput(ctx, "diff adopt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(entry, "0001-") {
		t.Fatalf("first snapshot entry = %q, want a 0001- prefix", entry)
	}
	if !strings.Contains(entry, "diff-adopt") {
		t.Fatalf("snapshot entry %q should carry the slugified reason", entry)
	}
	treeDir := filepath.Join(InputHistoryDir(ctx), entry, inputSnapshotTreeName)
	mustExist(t, filepath.Join(treeDir, "environment.yaml"))
	mustExist(t, filepath.Join(treeDir, "clusters", "storage", "ceph", "cluster.yaml"))
	mustExist(t, filepath.Join(InputHistoryDir(ctx), entry, inputSnapshotMetaName))

	meta, err := os.ReadFile(filepath.Join(InputHistoryDir(ctx), entry, inputSnapshotMetaName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"reason: diff adopt", "sequence: 1", "context: lab"} {
		if !strings.Contains(string(meta), want) {
			t.Fatalf("snapshot metadata missing %q:\n%s", want, meta)
		}
	}
}

func TestSnapshotInputSkipsWhenNoInputTree(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	ctx, err := NewContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	// No EnsureDirs: the input directory does not exist yet.
	entry, err := SnapshotInput(ctx, "never-applied")
	if err != nil {
		t.Fatalf("snapshot of a never-applied context should not error: %v", err)
	}
	if entry != "" {
		t.Fatalf("snapshot entry = %q, want empty for a context with no input tree", entry)
	}
}

func TestSnapshotSequenceIncrements(t *testing.T) {
	ctx := newTestContextWithInput(t)
	seedInput(t, ctx, "environment.yaml", "kind: Environment\n")
	first, err := SnapshotInput(ctx, "one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := SnapshotInput(ctx, "two")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, "0001-") || !strings.HasPrefix(second, "0002-") {
		t.Fatalf("sequences did not increment: first=%q second=%q", first, second)
	}
}

func TestPruneInputHistoryKeepsMostRecent(t *testing.T) {
	historyDir := t.TempDir()
	// Create more entries than the retention keep.
	for i := 1; i <= 5; i++ {
		if err := os.Mkdir(filepath.Join(historyDir, padSeq(i)+"-x"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := pruneInputHistory(historyDir, 3); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(historyDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("kept %d entries, want 3", len(entries))
	}
	// The three most recent (highest sequence) survive; the oldest two are gone.
	for _, gone := range []string{padSeq(1) + "-x", padSeq(2) + "-x"} {
		if _, err := os.Stat(filepath.Join(historyDir, gone)); !os.IsNotExist(err) {
			t.Fatalf("prune kept old entry %s", gone)
		}
	}
	mustExist(t, filepath.Join(historyDir, padSeq(5)+"-x"))
}

func padSeq(n int) string {
	s := "0000" + itoa(n)
	return s[len(s)-4:]
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestApplyInputEditsWritesAndSnapshots(t *testing.T) {
	ctx := newTestContextWithInput(t)
	seedInput(t, ctx, "clusters/storage/ceph/cluster.yaml", "kind: StorageCluster\n# authored comment\n")

	edits := []InputEdit{
		// Replace an existing authored file.
		{RelPath: "clusters/storage/ceph/cluster.yaml", Content: []byte("kind: StorageCluster\n# adopted\n")},
		// Create a brand-new object file in a new directory.
		{RelPath: "clusters/storage/ceph/pools/backups.yaml", Content: []byte("kind: StoragePool\n")},
	}
	snapshot, err := ApplyInputEdits(ctx, "diff adopt", edits)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == "" {
		t.Fatal("ApplyInputEdits returned an empty snapshot despite an existing input tree")
	}
	// The pre-edit content is preserved in history.
	snapPath := filepath.Join(InputHistoryDir(ctx), snapshot, inputSnapshotTreeName, "clusters", "storage", "ceph", "cluster.yaml")
	old, err := os.ReadFile(snapPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(old), "authored comment") {
		t.Fatalf("history snapshot lost the pre-edit content:\n%s", old)
	}
	// The edits landed.
	got, err := os.ReadFile(filepath.Join(ctx.InputDir, "clusters", "storage", "ceph", "cluster.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "adopted") {
		t.Fatalf("edit did not replace the file:\n%s", got)
	}
	mustExist(t, filepath.Join(ctx.InputDir, "clusters", "storage", "ceph", "pools", "backups.yaml"))
	if mode := statMode(t, filepath.Join(ctx.InputDir, "clusters", "storage", "ceph", "pools", "backups.yaml")); mode != 0o600 {
		t.Fatalf("adopted file mode = %#o, want 0600", mode)
	}
}

func TestApplyInputEditsRejectsEscape(t *testing.T) {
	ctx := newTestContextWithInput(t)
	seedInput(t, ctx, "environment.yaml", "kind: Environment\n")

	for _, bad := range []string{"../escape.yaml", "/abs.yaml", "a/../../escape.yaml", ""} {
		_, err := ApplyInputEdits(ctx, "adopt", []InputEdit{{RelPath: bad, Content: []byte("x")}})
		if err == nil {
			t.Fatalf("ApplyInputEdits accepted an escaping path %q", bad)
		}
		// A rejected edit must not have created a snapshot side effect beyond the
		// validation error; the escaping file must never exist.
		if _, statErr := os.Stat(filepath.Join(ctx.BaseDir, "escape.yaml")); !os.IsNotExist(statErr) {
			t.Fatalf("escaping edit %q wrote outside the input dir", bad)
		}
	}
}

func TestReplaceInputSnapshotsPriorTree(t *testing.T) {
	ctx := newTestContextWithInput(t)
	seedInput(t, ctx, "environment.yaml", "kind: Environment\n# original\n")

	source := t.TempDir()
	writeFile(t, filepath.Join(source, "environment.yaml"), "kind: Environment\n# replacement\n")

	snapshot, err := ReplaceInput(ctx, source, "context update", nil)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == "" {
		t.Fatal("ReplaceInput returned empty snapshot despite an existing input tree")
	}
	// New input is in place.
	got, err := os.ReadFile(filepath.Join(ctx.InputDir, "environment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "replacement") {
		t.Fatalf("ReplaceInput did not install the new tree:\n%s", got)
	}
	// The prior tree is recoverable from history.
	old, err := os.ReadFile(filepath.Join(InputHistoryDir(ctx), snapshot, inputSnapshotTreeName, "environment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(old), "original") {
		t.Fatalf("history did not preserve the pre-update tree:\n%s", old)
	}
}
