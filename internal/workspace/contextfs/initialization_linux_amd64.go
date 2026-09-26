//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"syscall"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func (t *transaction) Reserve(ctx context.Context, name, environment string, config []byte) (contexts.Record, error) {
	if err := t.available(ctx); err != nil {
		return contexts.Record{}, err
	}
	if !contextName(name) || environment != "" && (!canonicalPath(environment) || beneath(environment, t.root.path)) || len(config) == 0 || len(config) > maxRecord {
		return contexts.Record{}, state("context reservation request is invalid")
	}
	configuration, err := contexts.ParseConfiguration(name, config)
	if err != nil {
		return contexts.Record{}, err
	}
	if !bytes.Equal(configuration.Canonical(), config) {
		return contexts.Record{}, state("context configuration is not canonical")
	}
	var record contexts.Record
	for _, existing := range t.registry.Contexts {
		if existing.Name == name {
			record = existing
			break
		}
	}
	fresh := record.Name == ""
	if !fresh && (record.Mode != contexts.Initializing || record.SecretStoreType != configuration.SecretStore.Type) {
		return contexts.Record{}, state("context name is already reserved")
	}
	if !fresh {
		if err := t.syncIntent(ctx); err != nil {
			return contexts.Record{}, err
		}
	}
	if fresh {
		if len(t.registry.Contexts) >= maxContexts {
			return contexts.Record{}, state("active context limit exceeded")
		}
		registry := cloneRegistry(t.registry)
		record = contexts.Record{Name: name, EnvironmentDirectory: environment, Mode: contexts.Initializing, SecretStoreType: configuration.SecretStore.Type}
		registry.Contexts = append(registry.Contexts, record)
		if err := t.save(ctx, registry); err != nil {
			return contexts.Record{}, err
		}
		if err := t.store.checkpoint(ctx, "after-context-reservation"); err != nil {
			return contexts.Record{}, err
		}
	}
	if t.container == nil {
		container, err := t.store.ensureDirectory(ctx, t.root, "contexts")
		if err != nil {
			return contexts.Record{}, err
		}
		t.container = container
	}
	dir, err := openDirectory(t.container, name)
	if errors.Is(err, syscall.ENOENT) && record.DirectoryInode == 0 {
		dir, err = t.store.newDirectory(ctx, t.container, name)
		if err != nil {
			return contexts.Record{}, err
		}
		if err := t.store.checkpoint(ctx, "after-context-directory"); err != nil {
			dir.file.Close()
			return contexts.Record{}, err
		}
		runtime, makeErr := t.store.newDirectory(ctx, dir, "state")
		if makeErr == nil {
			data, _ := encodeRecord(reservation{Version: ReservationVersion, Name: name}, maxRecord)
			// The registry does not yet record this directory's identity, so
			// only its reservation lets a retry attribute it and record the
			// identity deletion requires: an interrupted write keeps it.
			_, makeErr = t.store.writeExclusiveIdentity(ctx, runtime, "reservation.json", data, true)
			runtime.file.Close()
		}
		if makeErr != nil {
			dir.file.Close()
			return contexts.Record{}, makeErr
		}
	} else if err == nil {
		if fresh {
			dir.file.Close()
			return contexts.Record{}, state("context directory already exists and cannot be adopted")
		}
		if record.DirectoryInode != 0 && (dir.identity.Ino != record.DirectoryInode || uint64(dir.identity.Dev) != record.DirectoryDevice) {
			dir.file.Close()
			return contexts.Record{}, state("initializing context directory was replaced")
		}
		if err = verifyReservation(ctx, dir, name); err != nil {
			dir.file.Close()
			return contexts.Record{}, err
		}
	}
	if err != nil {
		return contexts.Record{}, err
	}
	defer dir.file.Close()
	if err := verifyContextLayout(ctx, dir); err != nil {
		return contexts.Record{}, err
	}
	if record.DirectoryInode == 0 {
		record.DirectoryDevice = uint64(dir.identity.Dev)
		record.DirectoryInode = dir.identity.Ino
		registry := cloneRegistry(t.registry)
		for i := range registry.Contexts {
			if registry.Contexts[i].Name == record.Name {
				registry.Contexts[i] = record
			}
		}
		if err := t.save(ctx, registry); err != nil {
			return contexts.Record{}, err
		}
	}
	existing, err := readBounded(ctx, dir, "context.yaml", maxRecord, true)
	if errors.Is(err, syscall.ENOENT) {
		err = t.store.writeExclusive(ctx, dir, "context.yaml", config)
	} else if err == nil && !bytes.Equal(existing, config) {
		err = state("initializing context configuration does not match this retry")
	}
	if err != nil {
		return contexts.Record{}, err
	}
	for _, name := range []string{"desired-state", "secrets"} {
		child, err := t.store.ensureDirectory(ctx, dir, name)
		if err != nil {
			return contexts.Record{}, err
		}
		if name == "desired-state" {
			revisions, err := t.store.ensureDirectory(ctx, child, "revisions")
			if err != nil {
				child.file.Close()
				return contexts.Record{}, err
			}
			revisions.file.Close()
		}
		child.file.Close()
	}
	runtime, err := openDirectory(dir, "state")
	if err != nil {
		return contexts.Record{}, err
	}
	defer runtime.file.Close()
	evidence := []byte(pristineMutation)
	old, err := readBounded(ctx, runtime, "mutation.json", maxRecord, true)
	if errors.Is(err, syscall.ENOENT) {
		err = t.store.writeExclusive(ctx, runtime, "mutation.json", evidence)
	} else if err == nil && !bytes.Equal(old, evidence) {
		err = state("initializing context mutation evidence is not pristine")
	}
	if err != nil {
		return contexts.Record{}, err
	}
	return record, nil
}

func (t *transaction) Configuration(ctx context.Context, name string) ([]byte, error) {
	if err := t.available(ctx); err != nil {
		return nil, err
	}
	dir, err := t.contextDirectory(ctx, name)
	if err != nil {
		return nil, err
	}
	defer dir.file.Close()
	if err := verifyReservation(ctx, dir, name); err != nil {
		return nil, err
	}
	data, err := readBounded(ctx, dir, "context.yaml", maxRecord, true)
	if err != nil {
		return nil, err
	}
	record, err := t.record(name)
	if err != nil {
		return nil, err
	}
	parsed, err := contexts.ParseConfiguration(record.Name, data)
	if err != nil || !bytes.Equal(data, parsed.Canonical()) || parsed.SecretStore.Type != record.SecretStoreType {
		return nil, state("persisted context configuration is inconsistent")
	}
	return data, nil
}

func (t *transaction) InitializeSecrets(ctx context.Context, id string, callback func(secretstore.Area) error) error {
	if err := t.available(ctx); err != nil {
		return err
	}
	if callback == nil {
		return state("secret initialization callback is missing")
	}
	record, err := t.record(id)
	if err != nil {
		return err
	}
	if record.Mode != contexts.Initializing {
		return state("secret initialization requires an initializing context")
	}
	if _, err := t.MutationState(ctx, id); err != nil {
		return err
	}
	dir := t.leases[id]
	area := &secretArea{store: t.store, root: t.root, context: dir, expected: t.expected, token: secretContext(record), active: true, mutable: make(map[string]secretExpectation)}
	defer func() {
		area.close()
		if area.secrets != nil {
			area.secrets.file.Close()
		}
	}()
	area.secrets, err = openDirectory(dir, "secrets")
	if err != nil {
		return err
	}
	if _, _, err := area.scan(ctx); err != nil {
		return err
	}
	return callback(area)
}
