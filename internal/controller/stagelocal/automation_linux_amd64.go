//go:build linux && amd64

package stagelocal

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path"
	"slices"
	"sync"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// automationTree names the directory of a stage that holds the extracted
// automation, beside the lock and owner marker the sweep needs.
const automationTree = "tree"

// AutomationStager extracts the embedded automation tree into a stage of its
// own. Nothing in the tree is executed directly: the interpreter reads the
// supervisor and the modules as scripts, so the extracted files carry no
// execute bit and a noexec staging filesystem does not matter. The result's
// Lock is the exclusive lock on the tree: a process that inherits the
// descriptor keeps the tree out of every sweep, and Release removes the tree
// only while none does.
type AutomationStager struct {
	staging *Staging
	// source is what is written; a nil source is the embedded automation.
	// What is verified is always the embedded automation.
	source map[string][]byte
}

var _ prerequisites.AutomationStager = (*AutomationStager)(nil)

func NewAutomationStager(parent string) *AutomationStager {
	return &AutomationStager{staging: NewStaging(parent)}
}

// Stage writes the tree, reads it back and holds it to ansible.Automation,
// whose files the automation digest covers, so a tree that is staged is a tree
// that matches ansible.Digest. The exclusive lock is the stage's own and is
// meant to be inherited by every process that runs from the tree.
func (a *AutomationStager) Stage(ctx context.Context) (prerequisites.StagedAutomation, error) {
	if err := ctx.Err(); err != nil {
		return prerequisites.StagedAutomation{}, err
	}
	if a == nil {
		return prerequisites.StagedAutomation{}, stagingRefused()
	}
	parent, made, err := a.staging.open(automationKind)
	if err != nil {
		return prerequisites.StagedAutomation{}, err
	}
	defer parent.close()
	var once sync.Once
	release := func() { once.Do(func() { parent.releaseAutomation(made) }) }
	failed := func(code, message, correction string) (prerequisites.StagedAutomation, error) {
		release()
		return prerequisites.StagedAutomation{}, &prerequisites.ScopedFailure{Code: code, Message: message, Correction: correction}
	}
	stage, err := os.OpenRoot(path.Join(parent.path, made.name))
	if err != nil {
		return failed("controller.install", "the controller automation cannot be staged in "+parent.path, "Free space on the filesystem holding "+parent.path)
	}
	defer stage.Close()
	files := a.source
	if files == nil {
		files = ansible.Automation()
	}
	if err := writeTree(stage, files); err != nil {
		return failed("controller.install", "the controller automation cannot be staged in "+parent.path, "Free space on the filesystem holding "+parent.path)
	}
	if err := verifyTree(stage, ansible.Automation()); err != nil {
		return failed("controller.identity", "the staged automation does not match the embedded automation digest", "Rerun the command, and reinstall this Bootwright build if it repeats")
	}
	return prerequisites.StagedAutomation{Path: path.Join(parent.path, made.name, automationTree), Digest: ansible.Digest(), Lock: made.lock, Release: release}, nil
}

// releaseAutomation lets this invocation's lock go and then removes the tree
// only if no descendant still holds it through an inherited descriptor, which
// leaves it to the first sweep after the last holder ends.
func (p *stagingParent) releaseAutomation(made created) {
	made.lock.Close()
	root, err := os.OpenRoot(p.path)
	if err != nil {
		return
	}
	defer root.Close()
	reopened := &stagingParent{path: p.path, root: root, owner: p.owner, device: p.device, entries: p.entries}
	_, _ = reopened.reap(made.name, nil)
}

func writeTree(stage *os.Root, files map[string][]byte) error {
	if err := stage.Mkdir(automationTree, 0700); err != nil {
		return err
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		file := path.Join(automationTree, name)
		if err := stage.MkdirAll(path.Dir(file), 0700); err != nil {
			return err
		}
		if err := stage.WriteFile(file, files[name], 0400); err != nil {
			return err
		}
	}
	return nil
}

var errTreeMismatch = errors.New("staged automation differs from the embedded automation")

// verifyTree reads the extracted tree back through the stage's own handle. It
// holds exactly the expected regular files and the directories that hold them,
// each file private and carrying no execute bit and each byte equal, so the
// digest over the tree is the digest of the embedded automation.
func verifyTree(stage *os.Root, expected map[string][]byte) error {
	directories := map[string]bool{automationTree: true}
	for name := range expected {
		for dir := path.Dir(path.Join(automationTree, name)); dir != "."; dir = path.Dir(dir) {
			directories[dir] = true
		}
	}
	files := 0
	err := fs.WalkDir(stage.FS(), automationTree, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if !directories[name] {
				return errTreeMismatch
			}
			return nil
		}
		want, found := expected[name[len(automationTree)+1:]]
		info, statErr := entry.Info()
		if !found || statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0177 != 0 || info.Size() != int64(len(want)) {
			return errTreeMismatch
		}
		got, err := stage.ReadFile(name)
		if err != nil || !bytes.Equal(got, want) {
			return errTreeMismatch
		}
		files++
		return nil
	})
	if err != nil {
		return err
	}
	if files != len(expected) {
		return errTreeMismatch
	}
	return nil
}
