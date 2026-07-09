package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/host/safefs"
	"go.yaml.in/yaml/v3"
)

// A context owns its desired-state input tree (ctx.InputDir). Every command that
// mutates that tree — `context update` replacing it wholesale, `diff --adopt`
// rewriting individual objects, and future writers — must go through this one
// component so a single, consistent rule holds: the prior input is snapshotted
// into history before any change, and writes are confined to InputDir. Callers
// never write into InputDir directly; they hand this component the change to
// commit.

const (
	// InputHistoryName is the per-context directory that holds point-in-time
	// snapshots of the input tree, one per mutation.
	InputHistoryName = "input-history"
	// inputHistoryRetention caps how many snapshots are kept; older entries are
	// pruned after each new snapshot so history does not grow without bound.
	inputHistoryRetention = 20
	// inputSnapshotTreeName / inputSnapshotMetaName split each history entry into
	// the copied tree and its metadata, so the metadata never intermixes with the
	// snapshotted files (and a restore is just a copy of the tree subdirectory).
	inputSnapshotTreeName = "tree"
	inputSnapshotMetaName = "snapshot.yaml"
)

// InputEdit is one desired-state file to create or replace within a context's
// input tree, addressed by a slash-separated path relative to InputDir. It is
// the unit `diff --adopt` (and any future object-level writer) hands to
// ApplyInputEdits; the caller renders the file bytes (e.g. via a comment-
// preserving yaml.Node round-trip) and this component only commits them.
type InputEdit struct {
	RelPath string
	Content []byte
}

// InputSnapshotMeta records why and when a history entry was taken. It is
// written alongside the copied tree so `context` tooling can later list or
// restore snapshots with human context.
type InputSnapshotMeta struct {
	Sequence  int       `yaml:"sequence"`
	Reason    string    `yaml:"reason"`
	Context   string    `yaml:"context"`
	CreatedAt time.Time `yaml:"createdAt"`
}

// InputHistoryDir returns the history root for a context. It is a sibling of
// InputDir under BaseDir, so snapshots survive an input replacement and are
// removed with the context.
func InputHistoryDir(ctx Context) string {
	return filepath.Join(ctx.BaseDir, InputHistoryName)
}

// SnapshotInput copies the context's current input tree into a new history entry
// and prunes entries beyond the retention limit. It returns the created entry's
// directory name, or "" when there was no input tree to snapshot (a freshly
// initialized context). Reason is a short human label recorded in the entry
// metadata and slugified into its directory name.
func SnapshotInput(ctx Context, reason string) (string, error) {
	if strings.TrimSpace(ctx.InputDir) == "" {
		return "", fmt.Errorf("context %q has no input directory", ctx.Name)
	}
	info, err := os.Lstat(ctx.InputDir)
	if errors.Is(err, os.ErrNotExist) {
		// Nothing applied yet: no prior input to preserve.
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("stat input directory %s: %w", ctx.InputDir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("input directory %s is not a directory", ctx.InputDir)
	}

	historyDir := InputHistoryDir(ctx)
	if err := safefs.EnsureDir(historyDir, 0o700); err != nil {
		return "", err
	}
	seq, err := nextSnapshotSequence(historyDir)
	if err != nil {
		return "", err
	}
	entryName := fmt.Sprintf("%04d-%s", seq, slugifyReason(reason))
	entryDir := filepath.Join(historyDir, entryName)
	if err := os.Mkdir(entryDir, 0o700); err != nil {
		return "", fmt.Errorf("create snapshot directory %s: %w", entryDir, err)
	}
	treeDir := filepath.Join(entryDir, inputSnapshotTreeName)
	if err := os.Mkdir(treeDir, 0o700); err != nil {
		_ = os.RemoveAll(entryDir)
		return "", fmt.Errorf("create snapshot tree %s: %w", treeDir, err)
	}
	if err := copyInputTree(ctx.InputDir, treeDir); err != nil {
		_ = os.RemoveAll(entryDir)
		return "", fmt.Errorf("snapshot input tree: %w", err)
	}
	meta := InputSnapshotMeta{Sequence: seq, Reason: strings.TrimSpace(reason), Context: ctx.Name, CreatedAt: time.Now().UTC()}
	data, err := yaml.Marshal(meta)
	if err != nil {
		_ = os.RemoveAll(entryDir)
		return "", fmt.Errorf("marshal snapshot metadata: %w", err)
	}
	if err := safefs.AtomicWriteFile(filepath.Join(entryDir, inputSnapshotMetaName), data, 0o600); err != nil {
		_ = os.RemoveAll(entryDir)
		return "", err
	}
	if err := pruneInputHistory(historyDir, inputHistoryRetention); err != nil {
		return "", err
	}
	return entryName, nil
}

// ApplyInputEdits snapshots the current input, then writes each edit atomically
// under InputDir (creating parent directories), replacing an existing file or
// creating a new one. It never removes files — additive-only, matching the
// storage domain's philosophy — and rejects any relative path that escapes
// InputDir. It is the single entry point for object-level desired-state rewrites
// such as `diff --adopt`. The returned snapshot name identifies the pre-edit
// history entry (empty only when there was no prior input tree).
func ApplyInputEdits(ctx Context, reason string, edits []InputEdit) (string, error) {
	if err := ValidateName(ctx.Name); err != nil {
		return "", err
	}
	if strings.TrimSpace(ctx.InputDir) == "" {
		return "", fmt.Errorf("context %q has no input directory", ctx.Name)
	}
	if len(edits) == 0 {
		return "", errors.New("no input edits to apply")
	}
	// Resolve every target first so a bad path fails before we snapshot or write
	// anything (all-or-nothing validation, partial-write avoidance on obvious
	// errors).
	targets := make([]string, len(edits))
	for i, edit := range edits {
		target, err := resolveInputTarget(ctx.InputDir, edit.RelPath)
		if err != nil {
			return "", err
		}
		targets[i] = target
	}
	snapshot, err := SnapshotInput(ctx, reason)
	if err != nil {
		return "", err
	}
	for i, edit := range edits {
		if err := safefs.WriteFileEnsuringDir(targets[i], edit.Content, 0o600); err != nil {
			return snapshot, err
		}
	}
	return snapshot, nil
}

// ReplaceInput snapshots the current input, then replaces the whole tree from
// sourceDir (plus any referenced registered native add-ons). It is the entry
// point for `context update`, giving a wholesale replacement the same history
// guarantee as an object-level edit.
func ReplaceInput(ctx Context, sourceDir, reason string, addonDirs map[string]string) (string, error) {
	snapshot, err := SnapshotInput(ctx, reason)
	if err != nil {
		return "", err
	}
	if err := ReplaceInputDirWithAddons(ctx, sourceDir, addonDirs); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

// resolveInputTarget resolves a slash-separated relative edit path to an
// absolute path strictly inside InputDir, rejecting absolute paths, empty paths,
// and any traversal that would escape the input tree.
func resolveInputTarget(inputDir, relPath string) (string, error) {
	rel := strings.TrimSpace(relPath)
	if rel == "" {
		return "", errors.New("input edit path is required")
	}
	rel = filepath.FromSlash(rel)
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("input edit path %q must be relative to the context input", relPath)
	}
	target := filepath.Clean(filepath.Join(inputDir, rel))
	if target == filepath.Clean(inputDir) || !pathWithinOrSame(target, inputDir) {
		return "", fmt.Errorf("input edit path %q must resolve inside the context input directory", relPath)
	}
	return target, nil
}

var snapshotSlugRE = regexp.MustCompile(`[^a-z0-9]+`)

// slugifyReason turns a human reason into a filesystem-safe, deterministic slug
// for a snapshot directory name.
func slugifyReason(reason string) string {
	slug := snapshotSlugRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(reason)), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "update"
	}
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	return slug
}

// nextSnapshotSequence returns one past the highest leading sequence number
// among existing history entries (1 for an empty history), so entry names sort
// chronologically without relying on a wall clock.
func nextSnapshotSequence(historyDir string) (int, error) {
	entries, err := os.ReadDir(historyDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 1, nil
		}
		return 0, fmt.Errorf("read input history %s: %w", historyDir, err)
	}
	max := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if seq, ok := snapshotSequence(entry.Name()); ok && seq > max {
			max = seq
		}
	}
	return max + 1, nil
}

// snapshotSequence parses the leading NNNN- sequence from a history entry name.
func snapshotSequence(name string) (int, bool) {
	idx := strings.IndexByte(name, '-')
	if idx <= 0 {
		return 0, false
	}
	seq, err := strconv.Atoi(name[:idx])
	if err != nil {
		return 0, false
	}
	return seq, true
}

// pruneInputHistory removes the oldest entries so at most keep remain, by
// sequence order. Entries whose name does not carry a sequence are left alone.
func pruneInputHistory(historyDir string, keep int) error {
	entries, err := os.ReadDir(historyDir)
	if err != nil {
		return fmt.Errorf("read input history %s: %w", historyDir, err)
	}
	type seqEntry struct {
		seq  int
		name string
	}
	var sequenced []seqEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if seq, ok := snapshotSequence(entry.Name()); ok {
			sequenced = append(sequenced, seqEntry{seq: seq, name: entry.Name()})
		}
	}
	if len(sequenced) <= keep {
		return nil
	}
	sort.Slice(sequenced, func(i, j int) bool { return sequenced[i].seq < sequenced[j].seq })
	for _, entry := range sequenced[:len(sequenced)-keep] {
		if err := os.RemoveAll(filepath.Join(historyDir, entry.name)); err != nil {
			return fmt.Errorf("prune input history entry %s: %w", entry.name, err)
		}
	}
	return nil
}
