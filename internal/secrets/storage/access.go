package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type StoreAccess interface {
	Context(context.Context, string) (ContextSnapshot, error)
	Types() []string
	View(context.Context, Context, bool, func(StoreSession, Selection) error) error
	Mutate(context.Context, Context, func(StoreSession, Selection) error) error
	Initialize(context.Context, Context, string, func(StoreSession, Selection, bool) error) error
}

type Access struct {
	workspace Workspace
	catalog   *ImplementationCatalog
	material  SessionMaterialSource
}

func NewAccess(workspace Workspace, catalog *ImplementationCatalog, material SessionMaterialSource) *Access {
	return &Access{workspace: workspace, catalog: catalog, material: material}
}

func (a *Access) Types() []string {
	if a == nil {
		return []string{}
	}
	return a.catalog.Types()
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
			return Failure("store.uninitialized", "initialize the secret store with an explicit type first")
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
	implementation, err := a.catalog.Select(kind)
	if err != nil {
		return err
	}
	if selected.Mode != "active" {
		return Failure("store.conflict", "secret initialization requires an active context")
	}
	return a.workspace.MutateSecrets(ctx, selected, func(area Area) error {
		// Only explicit initialization may ask its selected implementation to
		// recover attributable never-published state. Reads still reject it.
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
	})
}

func (a *Access) open(ctx context.Context, selected Context, area Area, selector Selector, unlock bool, callback func(StoreSession, Selection) error) error {
	implementation, err := a.catalog.Reopen(selector.Selection)
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
