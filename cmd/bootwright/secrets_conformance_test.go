//go:build linux && amd64

package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/encryption"
	"github.com/crmarques/bootwright/internal/secrets/localstore"
	secretmaterial "github.com/crmarques/bootwright/internal/secrets/material"
	"github.com/crmarques/bootwright/internal/secrets/storage"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
)

// This test-only backend keeps material in memory. Its caller-owned session
// token demonstrates an opaque unlock seam, not a production password scheme.
type memoryImplementation struct {
	mu     sync.Mutex
	token  *memoryToken
	next   int
	states map[string]*memoryState
}

type memoryToken struct{ marker byte }

type memoryState struct {
	versions map[string]storage.Version
	material map[string]secrets.Material
	current  map[string]string
	bindings map[string]storage.Binding
	key      string
}

type memoryUnlock struct {
	token  *memoryToken
	closed bool
}

func (m *memoryUnlock) Close() error { m.closed = true; return nil }
func (*memoryUnlock) MarshalJSON() ([]byte, error) {
	return nil, errors.New("unlock capabilities are not serializable")
}
func (*memoryUnlock) String() string { return "<confidential session capability>" }

type memorySource struct {
	token        *memoryToken
	capabilities []*memoryUnlock
}

func (s *memorySource) Acquire(_ context.Context, _ storage.Context, _ storage.Selection, requirements []storage.SessionRequirement) (storage.SessionMaterial, error) {
	if !slices.Equal(requirements, []storage.SessionRequirement{{Kind: "operator-session"}}) {
		return nil, errors.New("unexpected session requirement")
	}
	capability := &memoryUnlock{token: s.token}
	s.capabilities = append(s.capabilities, capability)
	return capability, nil
}

func (*memoryImplementation) Selection() storage.Selection {
	return storage.Selection{Type: "session-test", Store: storage.ComponentRef{ID: "memory-test", InterfaceVersion: 1, StateVersion: 1, ConfigVersion: 1}, KeyCustody: storage.ComponentRef{ID: "operator-test", InterfaceVersion: 1, StateVersion: 1, ConfigVersion: 1}}
}

func (*memoryImplementation) Requirements() []storage.SessionRequirement {
	return []storage.SessionRequirement{{Kind: "operator-session"}}
}

func (i *memoryImplementation) Initialize(ctx context.Context, selected storage.Context, area storage.Area, capability storage.SessionMaterial) (storage.StoreSession, error) {
	i.mu.Lock()
	state := &memoryState{versions: map[string]storage.Version{}, material: map[string]secrets.Material{}, current: map[string]string{}, bindings: map[string]storage.Binding{}, key: "memory-key"}
	session := &memorySession{implementation: i, context: selected, area: area, state: state, unlocked: i.accepts(capability)}
	if err := session.authorized(); err != nil {
		session.Close()
		return nil, err
	}
	if err := session.publish(ctx); err != nil {
		session.Close()
		return nil, err
	}
	return session, nil
}

func (i *memoryImplementation) Open(ctx context.Context, selected storage.Context, area storage.Area, selector storage.Selector, capability storage.SessionMaterial) (storage.StoreSession, error) {
	i.mu.Lock()
	state, exists := i.states[selector.Generation]
	if !exists || selector.Selection != i.Selection() || selector.ContextID != selected.ID {
		i.mu.Unlock()
		return nil, storage.Failure("store.corrupt", "test snapshot is unavailable")
	}
	data, exists, err := area.ReadMutable(ctx, "selector.json", 64<<10)
	if err != nil || !exists {
		i.mu.Unlock()
		return nil, storage.Failure("store.corrupt", "test selector is unavailable")
	}
	return &memorySession{implementation: i, context: selected, area: area, state: state.clone(), selector: data, unlocked: i.accepts(capability)}, nil
}

func (i *memoryImplementation) accepts(capability storage.SessionMaterial) bool {
	value, ok := capability.(*memoryUnlock)
	return ok && !value.closed && value.token == i.token
}

func (i *memoryImplementation) id(prefix string) string {
	i.next++
	return fmt.Sprintf("%s-%010d", prefix, i.next)
}

type memorySession struct {
	implementation *memoryImplementation
	context        storage.Context
	area           storage.Area
	state          *memoryState
	selector       []byte
	unlocked       bool
	closed         bool
}

func (s *memorySession) authorized() error {
	if s.closed || !s.unlocked {
		return storage.Failure("store.key-unavailable", "test session is locked")
	}
	return nil
}

func (s *memorySession) publish(ctx context.Context) error {
	if err := s.authorized(); err != nil {
		return err
	}
	generation := s.implementation.id("generation")
	selector, err := storage.EncodeCanonical(storage.Selector{SelectorVersion: 1, ContextID: s.context.ID, Selection: s.implementation.Selection(), Generation: generation})
	if err != nil {
		return err
	}
	outcome, err := s.area.Replace(ctx, "selector.json", selector, s.selector)
	if err != nil || outcome != storage.Committed {
		return storage.Failure("store.conflict", "test publication failed")
	}
	s.state.collect()
	s.implementation.states[generation] = s.state.clone()
	s.selector = selector
	return nil
}

func (s *memorySession) Inspect(ctx context.Context) (storage.Snapshot, error) {
	result := storage.Snapshot{Versions: []storage.Version{}, Current: []storage.Current{}, Bindings: []storage.Binding{}, ActiveKey: s.state.key, Keys: []storage.Key{{ID: s.state.key, State: "active"}}}
	for _, version := range s.state.versions {
		result.Versions = append(result.Versions, version)
	}
	for name, version := range s.state.current {
		result.Current = append(result.Current, storage.Current{Name: name, Version: version})
	}
	for _, binding := range s.state.bindings {
		result.Bindings = append(result.Bindings, binding)
	}
	slices.SortFunc(result.Versions, func(a, b storage.Version) int { return compareTestID(a.ID, b.ID) })
	slices.SortFunc(result.Current, func(a, b storage.Current) int { return compareTestID(a.Name, b.Name) })
	slices.SortFunc(result.Bindings, func(a, b storage.Binding) int { return compareTestID(a.ID, b.ID) })
	return result, ctx.Err()
}

func compareTestID(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func (s *memorySession) Read(ctx context.Context, id string) (secrets.Material, error) {
	if err := s.authorized(); err != nil {
		return secrets.Material{}, err
	}
	if value, exists := s.state.material[id]; exists {
		return cloneTestMaterial(value), ctx.Err()
	}
	return secrets.Material{}, storage.Failure("input", "test version is absent")
}

func (s *memorySession) add(declaration secrets.Declaration, material secrets.Material) storage.Version {
	version := storage.Version{ID: s.implementation.id("version"), Declaration: declaration, Parts: material.Parts()}
	s.state.versions[version.ID] = version
	s.state.material[version.ID] = cloneTestMaterial(material)
	return version
}

func (s *memorySession) PutBatch(ctx context.Context, puts []storage.Put) ([]storage.Version, error) {
	if err := s.authorized(); err != nil {
		return nil, err
	}
	versions := make([]storage.Version, 0, len(puts))
	for _, put := range puts {
		version := s.add(put.Declaration, put.Material)
		s.state.current[put.Declaration.Name] = version.ID
		versions = append(versions, version)
	}
	return versions, s.publish(ctx)
}

func (s *memorySession) Delete(ctx context.Context, name string) (bool, error) {
	if err := s.authorized(); err != nil {
		return false, err
	}
	if _, exists := s.state.current[name]; !exists {
		return false, nil
	}
	delete(s.state.current, name)
	return true, s.publish(ctx)
}

func (s *memorySession) Bind(ctx context.Context, inputs []storage.BoundInput) (storage.Binding, error) {
	if err := s.authorized(); err != nil {
		return storage.Binding{}, err
	}
	binding := storage.Binding{ID: s.implementation.id("binding"), Versions: []string{}}
	for _, input := range inputs {
		id := input.Version
		if id == "" {
			id = s.add(input.Declaration, input.Material).ID
		}
		binding.Versions = append(binding.Versions, id)
	}
	slices.Sort(binding.Versions)
	s.state.bindings[binding.ID] = binding
	return binding, s.publish(ctx)
}

func (s *memorySession) Reopen(ctx context.Context, id string) ([]storage.BoundMaterial, error) {
	binding, exists := s.state.bindings[id]
	if !exists {
		return nil, storage.Failure("input", "test binding is absent")
	}
	result := []storage.BoundMaterial{}
	for _, version := range binding.Versions {
		material, err := s.Read(ctx, version)
		if err != nil {
			for _, item := range result {
				item.Material.Clear()
			}
			return nil, err
		}
		result = append(result, storage.BoundMaterial{Version: s.state.versions[version], Material: material})
	}
	return result, nil
}

func (s *memorySession) Release(ctx context.Context, id string) (bool, error) {
	if err := s.authorized(); err != nil {
		return false, err
	}
	if _, exists := s.state.bindings[id]; !exists {
		return false, nil
	}
	delete(s.state.bindings, id)
	return true, s.publish(ctx)
}

func (s *memorySession) Rotate(ctx context.Context) (string, error) {
	if err := s.authorized(); err != nil {
		return "", err
	}
	s.state.key = s.implementation.id("key")
	return s.state.key, s.publish(ctx)
}

func (s *memorySession) Close() error {
	if !s.closed {
		s.state.clear()
		s.closed = true
		s.implementation.mu.Unlock()
	}
	return nil
}

func cloneTestMaterial(value secrets.Material) secrets.Material {
	parts := make(map[secrets.Part][]byte)
	for _, part := range value.Parts() {
		parts[part], _ = value.Part(part)
	}
	result := secrets.NewMaterial(parts)
	for _, data := range parts {
		clear(data)
	}
	return result
}

func (s *memoryState) clone() *memoryState {
	result := &memoryState{versions: map[string]storage.Version{}, material: map[string]secrets.Material{}, current: map[string]string{}, bindings: map[string]storage.Binding{}, key: s.key}
	for id, version := range s.versions {
		version.Parts = slices.Clone(version.Parts)
		version.Declaration.Generation.DNSNames = slices.Clone(version.Declaration.Generation.DNSNames)
		version.Declaration.Generation.IPAddresses = slices.Clone(version.Declaration.Generation.IPAddresses)
		result.versions[id] = version
		result.material[id] = cloneTestMaterial(s.material[id])
	}
	for name, id := range s.current {
		result.current[name] = id
	}
	for id, binding := range s.bindings {
		binding.Versions = slices.Clone(binding.Versions)
		result.bindings[id] = binding
	}
	return result
}

func (s *memoryState) collect() {
	used := map[string]bool{}
	for _, id := range s.current {
		used[id] = true
	}
	for _, binding := range s.bindings {
		for _, id := range binding.Versions {
			used[id] = true
		}
	}
	for id := range s.versions {
		if !used[id] {
			s.material[id].Clear()
			delete(s.material, id)
			delete(s.versions, id)
		}
	}
}

func (s *memoryState) clear() {
	for _, value := range s.material {
		value.Clear()
	}
}

func withMemoryStore(t *testing.T, services cli.Services, repository *contextfs.Store) cli.Services {
	t.Helper()
	token := &memoryToken{marker: 1}
	implementation := &memoryImplementation{token: token, states: map[string]*memoryState{}}
	source := &memorySource{token: token}
	access := storage.NewAccess(repository, storage.NewCatalog(localstore.New(), implementation), source)
	services.Secrets = custody.New(access, wireCompiler(), secretmaterial.New(nil), nil)
	services.Encryption = encryption.New(access, nil)
	t.Cleanup(func() {
		if len(source.capabilities) == 0 {
			t.Error("selected implementation never acquired its session capability")
		}
		for _, capability := range source.capabilities {
			if !capability.closed {
				t.Error("session capability was not closed")
			}
			if _, err := capability.MarshalJSON(); err == nil {
				t.Error("session capability became serializable")
			}
		}
		for _, state := range implementation.states {
			state.clear()
		}
	})
	return services
}
