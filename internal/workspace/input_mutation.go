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

const (
	InputHistoryName      = "input-history"
	inputHistoryRetention = 20
	inputSnapshotTreeName = "tree"
	inputSnapshotMetaName = "snapshot.yaml"
)

type InputEdit struct {
	RelPath string
	Content []byte
}

type InputSnapshotMeta struct {
	Sequence  int       `yaml:"sequence"`
	Reason    string    `yaml:"reason"`
	Context   string    `yaml:"context"`
	CreatedAt time.Time `yaml:"createdAt"`
}

func InputHistoryDir(ctx Context) string {
	return filepath.Join(ctx.BaseDir, InputHistoryName)
}

func SnapshotInput(ctx Context, reason string) (string, error) {
	if strings.TrimSpace(ctx.InputDir) == "" {
		return "", fmt.Errorf("context %q has no input directory", ctx.Name)
	}
	info, err := os.Lstat(ctx.InputDir)
	if errors.Is(err, os.ErrNotExist) {
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
