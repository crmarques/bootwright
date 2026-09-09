package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type ImplementationResolver interface {
	Types() []string
	Select(string) (SecretStoreImplementation, error)
	Reopen(Selection) (SecretStoreImplementation, error)
}

type Access struct {
	workspace Workspace
	resolver  ImplementationResolver
	material  SessionMaterialSource
}

func NewAccess(workspace Workspace, resolver ImplementationResolver, material SessionMaterialSource) *Access {
	return &Access{workspace: workspace, resolver: resolver, material: material}
}

func (a *Access) Types() []string {
	if a == nil || a.resolver == nil {
		return []string{}
	}
	return slices.Clone(a.resolver.Types())
}

func (a *Access) Context(ctx context.Context, name string) (ContextSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return ContextSnapshot{}, err
	}
	if a == nil || a.workspace == nil {
		return ContextSnapshot{}, Failure("store.implementation", "secret workspace is not configured")
	}
	return a.workspace.SecretContext(ctx, name)
}

func (a *Access) View(ctx context.Context, selected Context, unlock bool, callback func(StoreSession, Selection) error) error {
	if a == nil || a.workspace == nil {
		return Failure("store.implementation", "secret workspace is not configured")
	}
	return a.workspace.ReadSecrets(ctx, selected, func(area Area) error {
		selector, exists, err := ReadSelector(ctx, area, selected.ID)
		if err != nil {
			return err
		}
		if !exists {
			return callback(nil, Selection{})
		}
		return a.open(ctx, selected, area, selector, unlock, callback)
	})
}

func (a *Access) Mutate(ctx context.Context, selected Context, callback func(StoreSession, Selection) error) error {
	if a == nil || a.workspace == nil {
		return Failure("store.implementation", "secret workspace is not configured")
	}
	return a.workspace.MutateSecrets(ctx, selected, func(area Area) error {
		selector, exists, err := ReadSelector(ctx, area, selected.ID)
		if err != nil {
			return err
		}
		if !exists {
			return Failure("store.uninitialized", "run secret encryption init to initialize the configured store")
		}
		return a.open(ctx, selected, area, selector, true, callback)
	})
}

func (a *Access) Initialize(ctx context.Context, selected Context, kind string, callback func(StoreSession, Selection, bool) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a == nil || a.workspace == nil {
		return Failure("store.implementation", "secret workspace is not configured")
	}
	if selected.Mode != "ready" {
		return Failure("store.conflict", "secret initialization requires a ready context")
	}
	if a.resolver == nil || callback == nil {
		return Failure("store.implementation", "secret initialization is not configured")
	}
	implementation, err := a.resolver.Select(kind)
	if err != nil {
		return err
	}
	return a.workspace.MutateSecrets(ctx, selected, func(area Area) error {
		return a.initializeArea(ctx, selected, implementation, area, callback)
	})
}

// InitializeArea initializes only the supplied transaction-scoped area. The
// caller owns its Workspace lock and has authorized creation of this identity.
func (a *Access) InitializeArea(ctx context.Context, selected Context, kind string, area Area) error {
	if selected.Mode != "initializing" && selected.Mode != "ready" {
		return Failure("store.conflict", "secret initialization requires a creating or ready context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if a == nil || a.resolver == nil {
		return Failure("store.implementation", "secret initialization is not configured")
	}
	implementation, err := a.resolver.Select(kind)
	if err != nil {
		return err
	}
	return a.initializeArea(ctx, selected, implementation, area, func(StoreSession, Selection, bool) error { return nil })
}

func (a *Access) initializeArea(ctx context.Context, selected Context, implementation SecretStoreImplementation, area Area, callback func(StoreSession, Selection, bool) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a == nil || a.resolver == nil || implementation == nil || area == nil || callback == nil {
		return Failure("store.implementation", "secret initialization is not configured")
	}
	selector, exists, err := readSelectorRecord(ctx, area, selected.ID)
	if err != nil {
		return err
	}
	if exists {
		if selector.Selection != implementation.Selection() {
			return Failure("store.implementation", "initialization cannot change an existing store implementation")
		}
		return a.open(ctx, selected, area, selector, true, func(session StoreSession, selection Selection) error { return callback(session, selection, false) })
	}
	material, err := a.acquire(ctx, selected, implementation, true)
	if err != nil {
		return err
	}
	if material != nil {
		defer material.Close()
	}
	session, err := implementation.Initialize(ctx, selected, area, material)
	if err != nil {
		return err
	}
	if session == nil {
		return Failure("store.implementation", "implementation returned no initialized session")
	}
	defer session.Close()
	return callback(session, implementation.Selection(), true)
}

func (a *Access) open(ctx context.Context, selected Context, area Area, selector Selector, unlock bool, callback func(StoreSession, Selection) error) error {
	if a.resolver == nil {
		return Failure("store.implementation", "secret store implementation catalog is invalid")
	}
	implementation, err := a.resolver.Reopen(selector.Selection)
	if err != nil {
		return err
	}
	material, err := a.acquire(ctx, selected, implementation, unlock)
	if err != nil {
		return err
	}
	if material != nil {
		defer material.Close()
	}
	session, err := implementation.Open(ctx, selected, area, selector, material)
	if err != nil {
		return err
	}
	if session == nil {
		return Failure("store.implementation", "implementation returned no store session")
	}
	defer session.Close()
	return callback(session, selector.Selection)
}

func (a *Access) acquire(ctx context.Context, selected Context, implementation SecretStoreImplementation, unlock bool) (SessionMaterial, error) {
	requirements := slices.Clone(implementation.Requirements())
	if len(requirements) == 0 || !unlock {
		return nil, nil
	}
	if len(requirements) > 8 || a.material == nil {
		return nil, Failure("store.key-unavailable", "secret store requires an explicit unlock session")
	}
	material, err := a.material.Acquire(ctx, selected, implementation.Selection(), requirements)
	if err != nil || material == nil {
		if material != nil {
			material.Close()
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, Failure("store.key-unavailable", "secret store unlock session is unavailable")
	}
	return material, nil
}

func ReadSelector(ctx context.Context, area Area, contextID string) (Selector, bool, error) {
	selector, exists, err := readSelectorRecord(ctx, area, contextID)
	if err != nil || exists {
		return selector, exists, err
	}
	entries, err := area.Entries(ctx, "")
	if err != nil {
		return Selector{}, false, err
	}
	if len(entries) != 0 {
		return Selector{}, false, Failure("store.corrupt", "nonempty secret store has no selector; recovery is required")
	}
	return Selector{}, false, nil
}

func readSelectorRecord(ctx context.Context, area Area, contextID string) (Selector, bool, error) {
	data, exists, err := area.ReadMutable(ctx, "selector.json", 64<<10)
	if err != nil {
		return Selector{}, false, err
	}
	if !exists {
		return Selector{}, false, nil
	}
	var selector Selector
	if err := DecodeCanonical(data, &selector); err != nil || selector.SelectorVersion != 1 || selector.ContextID != contextID || !validSelection(selector.Selection) || !api.ValidLexical("name", selector.Generation) {
		return Selector{}, false, Failure("store.corrupt", "secret store selector is invalid or incompatible")
	}
	return selector, true, nil
}

func EncodeCanonical(value any) ([]byte, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, Failure("store.corrupt", "secret store record could not be encoded")
	}
	return append(b, '\n'), nil
}

func DecodeCanonical(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return errors.New("invalid private record")
	}
	canonical, err := EncodeCanonical(value)
	if err != nil || !bytes.Equal(canonical, data) {
		return errors.New("noncanonical private record")
	}
	return nil
}
