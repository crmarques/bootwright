package lifecycle

import (
	"context"
	"crypto/subtle"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func (s AdminCredentialService) AdministratorPassword(ctx context.Context, contextName, cluster, block, seed, login string) (secretstore.ProducedMaterial, bool, error) {
	read := func(output string) (secretstore.ProducedMaterial, bool, error) {
		return s.binder.ReadProduced(ctx, custody.ReadProducedRequest{ContextName: contextName, Block: block, Name: output})
	}
	active, found, err := read("admin-password")
	if err == nil && !found {
		active, found, err = read(seed)
	}
	if err != nil {
		active.Material.Clear()
		return secretstore.ProducedMaterial{}, false, err
	}
	pending, present, err := read("admin-password-update")
	defer pending.Material.Clear()
	if err != nil {
		active.Material.Clear()
		return secretstore.ProducedMaterial{}, false, err
	}
	if present {
		encoded, _ := pending.Material.Part(secrets.ValuePart)
		defer clear(encoded)
		var update adminUpdate
		valid := len(encoded) <= 16384 && canonicaljson.DecodeClosed(encoded, &update) == nil && s.validAdminUpdate(update) && !pending.Unproved
		password, _ := active.Material.Part(secrets.ValuePart)
		defer clear(password)
		current := valid && subtle.ConstantTimeCompare(password, []byte(update.Password)) == 1
		if !current {
			active.Material.Clear()
			return secretstore.ProducedMaterial{}, false, pendingUpdate(contextName, cluster, login)
		}
	}
	return active, found, nil
}

func pendingUpdate(contextName, cluster, login string) error {
	command := "bootwright cluster update-admin-creds --context " + contextName + " --name " + cluster
	subject := "the administrator credential update"
	if login != "" {
		command += " --login " + login
		subject = "the " + login + " administrator credential update"
	}
	return engineCredentialPending.Fail(subject+" has not completed in custody", command+" finishes the retained update")
}

func (s AdminCredentialService) MonitoringLogin(ctx context.Context, contextName, cluster, block, output, service string) (secretstore.ProducedMaterial, bool, error) {
	read, found, err := s.binder.ReadProduced(ctx, custody.ReadProducedRequest{ContextName: contextName, Block: block, Name: output})
	if err != nil || !found {
		return read, found, err
	}
	pending, present, err := s.binder.ReadProduced(ctx, custody.ReadProducedRequest{ContextName: contextName, Block: block, Name: service + "-update"})
	defer pending.Material.Clear()
	if err != nil {
		read.Material.Clear()
		return secretstore.ProducedMaterial{}, false, err
	}
	if !present {
		return read, true, nil
	}
	value, _ := read.Material.Part(secrets.ValuePart)
	logins, ok := decodeLogins(value)
	clear(value)
	entry, listed := logins[service]
	if !s.finished(pending, ok && listed, []byte(entry.Password), entry.User) {
		read.Material.Clear()
		return secretstore.ProducedMaterial{}, false, pendingUpdate(contextName, cluster, service)
	}
	return read, true, nil
}

func (s AdminCredentialService) GrafanaPassword(ctx context.Context, contextName, cluster, block, secret string) (secrets.Material, bool, error) {
	binding, err := s.blockBinding(ctx, contextName, block)
	if err != nil {
		return secrets.Material{}, false, err
	}
	var read []secretstore.BoundMaterial
	if binding != "" {
		read, err = s.binder.Reopen(ctx, custody.BindingRequest{ContextName: contextName, BindingID: binding})
	} else {
		read, err = s.binder.ReadCurrent(ctx, custody.ReadCurrentRequest{ContextName: contextName, Names: []string{secret}})
	}
	var value secrets.Material
	found := false
	for _, item := range read {
		if item.Version.Declaration.Name == secret && !found {
			value, found = item.Material, true
			continue
		}
		item.Material.Clear()
	}
	if err != nil || !found {
		value.Clear()
		return secrets.Material{}, false, err
	}
	pending, present, err := s.binder.ReadProduced(ctx, custody.ReadProducedRequest{ContextName: contextName, Block: block, Name: "grafana-update"})
	defer pending.Material.Clear()
	if err != nil {
		value.Clear()
		return secrets.Material{}, false, err
	}
	if present {
		password, ok := value.Part(secrets.ValuePart)
		finished := s.finished(pending, ok, password, "")
		clear(password)
		if !finished {
			value.Clear()
			return secrets.Material{}, false, pendingUpdate(contextName, cluster, "grafana")
		}
	}
	return value, true, nil
}

func (s AdminCredentialService) finished(pending secretstore.ProducedMaterial, readable bool, password []byte, user string) bool {
	encoded, _ := pending.Material.Part(secrets.ValuePart)
	defer clear(encoded)
	var update loginUpdate
	if pending.Unproved || !readable || decodeLoginUpdate(encoded, &update) != nil {
		return false
	}
	if user != "" && user != update.User {
		return false
	}
	return subtle.ConstantTimeCompare(password, []byte(update.Password)) == 1
}

func (s AdminCredentialService) blockBinding(ctx context.Context, contextName, block string) (string, error) {
	if s.workspace == nil || s.operations == nil {
		return "", nil
	}
	name, err := s.resolve(ctx, contextName)
	if err != nil {
		return "", err
	}
	binding := ""
	err = s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		store := s.store(view)
		index, err := store.Index(ctx)
		if err != nil || index.Current == "" {
			return err
		}
		operation, err := store.ReadOperation(ctx, index.Current)
		if err != nil || operation.Verb != reconciliation.Apply {
			return err
		}
		chain, err := appliedLineage(ctx, store, operation)
		if err != nil {
			return err
		}
		if _, held := chain.ownerOf(block); !held {
			return nil
		}
		binding, err = s.loginBinding(ctx, store, operation, block)
		return err
	})
	return binding, err
}

func AdminCredentialProved(evidence []byte, digest string) bool {
	var proof struct {
		Proved  bool   `json:"proved"`
		Request string `json:"request"`
	}
	return len(evidence) <= 512 && digest != "" && canonicaljson.DecodeClosed(evidence, &proof) == nil && proof.Proved && proof.Request == digest
}
