//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"syscall"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const maxInitializationStages = 16

func initializationStagePrefix(name string) string { return ".init-" + name + "-" }

func initializationDamage(name string, cause error) error {
	return contexts.StateErrorWithRemediation("initializing context entry contexts/"+name+" cannot be attributed: "+causeText(cause),
		"preserve the unattributed entry and restore a matching complete store backup, or have the administrator isolate that entry without deleting its contents; then finish with bootwright context init --name "+name+" and its original Context file, or discard the reserved name with bootwright context delete --name "+name+" --purge")
}

func (t *transaction) collectInitializationStages(ctx context.Context, name string) error {
	if err := t.writable(ctx); err != nil {
		return err
	}
	if t.container == nil {
		container, err := openDirectory(t.root, "contexts")
		if errors.Is(err, syscall.ENOENT) {
			return nil
		}
		if err != nil {
			return err
		}
		t.container = container
	}
	names, err := directoryNames(t.container, maxContexts+maxInitializationStages)
	if err != nil {
		return err
	}
	var stages []string
	for _, entry := range names {
		if identifier(entry, initializationStagePrefix(name)) {
			stages = append(stages, entry)
		}
	}
	if len(stages) > maxInitializationStages {
		return state("initialization stage limit exceeded")
	}
	for _, stage := range stages {
		if err := t.initializationStage(ctx, stage, false); err != nil {
			return err
		}
	}
	for _, stage := range stages {
		if err := t.initializationStage(ctx, stage, true); err != nil {
			return err
		}
	}
	return nil
}

func (t *transaction) initializationStage(ctx context.Context, name string, remove bool) error {
	dir, err := openDirectory(t.container, name)
	if err != nil {
		return unsafeEntry(t.container, name, err)
	}
	defer dir.file.Close()
	names, err := directoryNames(dir, 1)
	if err != nil || len(names) == 1 && names[0] != "state" {
		return state("initialization stage contains foreign entries: " + storeEntry(t.container, name))
	}
	if len(names) == 1 {
		runtime, err := openDirectory(dir, "state")
		if err != nil {
			return unsafeEntry(dir, "state", err)
		}
		defer runtime.file.Close()
		entries, err := directoryNames(runtime, 1)
		if err != nil || len(entries) == 1 && entries[0] != "reservation.json" {
			return state("initialization stage contains foreign state: " + storeEntry(dir, "state"))
		}
		if len(entries) == 1 {
			_, identity, err := readBoundedIdentity(ctx, runtime, "reservation.json", maxRecord, true)
			if err != nil {
				return unsafeEntry(runtime, "reservation.json", err)
			}
			if remove {
				if err := unlinkVerified(runtime, "reservation.json", identity, false); err != nil {
					return err
				}
				if err := t.store.syncDirectory(ctx, runtime); err != nil {
					return err
				}
			}
		}
		if remove {
			if err := unlinkVerified(dir, "state", runtime.identity, true); err != nil {
				return err
			}
			if err := t.store.syncDirectory(ctx, dir); err != nil {
				return err
			}
		}
	}
	if remove {
		if err := unlinkVerified(t.container, name, dir.identity, true); err != nil {
			return err
		}
		return t.store.syncDirectory(ctx, t.container)
	}
	return dir.verify()
}
