package custody

import (
	"context"
	"crypto/subtle"
	"slices"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/storage"
)

type Service struct {
	access    StoreAccess
	compiler  Compiler
	material  Materializer
	confirmer Confirmer
}

func New(access StoreAccess, compiler Compiler, material Materializer, confirmer Confirmer) *Service {
	return &Service{access: access, compiler: compiler, material: material, confirmer: confirmer}
}

type SetRequest struct {
	ContextName      string
	Name             string
	Input            secrets.Input
	SkipConfirmation bool
}
type GenerateRequest struct {
	ContextName string
	Name        string
	Renew       bool
}
type CheckRequest struct{ ContextName string }
type ListRequest struct{ ContextName string }
type ShowRequest struct {
	ContextName string
	Name        string
	Part        secrets.Part
}
type DeleteRequest struct {
	ContextName      string
	Name             string
	SkipConfirmation bool
}

func (s Service) Set(ctx context.Context, request SetRequest) (*MutationResult, error) {
	selected, declarations, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	if err = requireActive(selected); err != nil {
		return nil, err
	}
	d, err := findDeclaration(declarations, request.Name)
	if err != nil {
		return nil, err
	}
	if d.Source != "contextStore" {
		return nil, storage.Failure("source", "set requires a declared contextStore source")
	}
	if err = validateInput(d.Type, request.Input); err != nil {
		return nil, err
	}
	result := &MutationResult{Context: selected, Name: request.Name, Parts: d.Parts()}
	err = s.access.Mutate(ctx, selected, func(session storage.StoreSession, _ storage.Selection) error {
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		previous, exists := currentVersion(snapshot, request.Name)
		if exists && !request.SkipConfirmation {
			if request.Input.UsesStdin() {
				return storage.Failure("input", "stdin replacement requires --yes before reading material")
			}
			if err := s.confirm(ctx, "replace secret", request.Name); err != nil {
				return err
			}
		}
		material, err := s.material.Acquire(ctx, d, request.Input)
		if err != nil {
			return err
		}
		defer material.Clear()
		if exists && previous.Declaration.Fingerprint == d.Fingerprint {
			old, err := session.Read(ctx, previous.ID)
			if err != nil {
				return err
			}
			equal := equalMaterial(old, material)
			old.Clear()
			if equal {
				result.Unchanged = 1
				return nil
			}
		}
		_, err = session.PutBatch(ctx, []storage.Put{{Declaration: d, Material: material}})
		if err == nil {
			result.Changed = 1
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s Service) Generate(ctx context.Context, request GenerateRequest) (*MutationResult, error) {
	selected, declarations, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	if err = requireActive(selected); err != nil {
		return nil, err
	}
	if request.Name != "" {
		d, err := findDeclaration(declarations, request.Name)
		if err != nil {
			return nil, err
		}
		if d.Source != "generated" {
			return nil, storage.Failure("source", "generate requires a generated source declaration")
		}
		declarations = []secrets.Declaration{d}
	}
	result := &MutationResult{Context: selected, Name: request.Name}
	err = s.access.Mutate(ctx, selected, func(session storage.StoreSession, _ storage.Selection) error {
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		var writes []storage.Put
		materialBytes := 0
		defer func() {
			for _, write := range writes {
				write.Material.Clear()
			}
		}()
		for _, d := range declarations {
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.Source != "generated" {
				continue
			}
			previous, exists := currentVersion(snapshot, d.Name)
			if exists && previous.Declaration.Fingerprint == d.Fingerprint && !request.Renew {
				result.Unchanged++
				continue
			}
			if len(writes) >= secrets.MaxVersions {
				return storage.Failure("store.limit", "generated selection exceeds the logical version limit")
			}
			material, err := s.material.Generate(ctx, d)
			if err != nil {
				return err
			}
			if material.Size() > secrets.MaxMaterialBytes-materialBytes {
				material.Clear()
				return storage.Failure("store.limit", "generated selection exceeds the material byte limit")
			}
			materialBytes += material.Size()
			writes = append(writes, storage.Put{Declaration: d, Material: material})
		}
		if len(writes) == 0 {
			return nil
		}
		_, err = session.PutBatch(ctx, writes)
		if err == nil {
			result.Changed = len(writes)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s Service) Check(ctx context.Context, request CheckRequest) (*CheckResult, error) {
	selected, declarations, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	result := &CheckResult{Context: selected, Secrets: []CheckRow{}}
	var diagnostics []desiredstate.Diagnostic
	err = s.access.View(ctx, selected, true, func(session storage.StoreSession, _ storage.Selection) error {
		var snapshot storage.Snapshot
		if session != nil {
			var err error
			snapshot, err = session.Inspect(ctx)
			if err != nil {
				return err
			}
		}
		for _, d := range declarations {
			if err := ctx.Err(); err != nil {
				return err
			}
			row := CheckRow{Name: d.Name, Type: d.Type, Source: d.Source, Parts: d.Parts(), Status: "available"}
			var material secrets.Material
			var err error
			if d.Source == "file" {
				material, err = s.material.File(ctx, d)
				if err != nil {
					row.Status = "unreadable"
					for _, diagnostic := range desiredstate.DiagnosticsOf(err) {
						if diagnostic.Code == "secret.input" {
							row.Status = "invalid"
						}
					}
				}
			} else if version, exists := currentVersion(snapshot, d.Name); !exists {
				row.Status = "missing"
			} else {
				id := version.ID
				row.Version, row.Sequence = &id, version.Sequence
				if version.Declaration.Fingerprint != d.Fingerprint {
					row.Status = "stale"
				} else {
					material, err = session.Read(ctx, version.ID)
					if err != nil {
						return err
					}
				}
			}
			if row.Status == "available" {
				if err = s.material.Validate(ctx, d, material); err != nil {
					row.Status = "invalid"
				}
			}
			material.Clear()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if row.Status != "available" {
				diagnostics = append(diagnostics, desiredstate.Diagnostic{Severity: "error", Code: "secret.input", Message: "secret " + d.Name + " is " + row.Status})
			}
			result.Secrets = append(result.Secrets, row)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(diagnostics) > 0 {
		desiredstate.SortDiagnostics(diagnostics)
		return result, &desiredstate.Failure{Diagnostics: diagnostics}
	}
	return result, nil
}

func (s Service) List(ctx context.Context, request ListRequest) (*ListResult, error) {
	selected, declarations, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	result := &ListResult{Context: selected, Secrets: []ListRow{}}
	err = s.access.View(ctx, selected, true, func(session storage.StoreSession, _ storage.Selection) error {
		if session == nil {
			return nil
		}
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		bound := map[string]bool{}
		for _, b := range snapshot.Bindings {
			for _, id := range b.Versions {
				bound[id] = true
			}
		}
		rows := map[string]ListRow{}
		for _, v := range snapshot.Versions {
			d := v.Declaration
			if d.Source == "file" {
				continue
			}
			current, exists := currentVersion(snapshot, d.Name)
			if !bound[v.ID] && (!exists || current.ID != v.ID) {
				continue
			}
			row, present := rows[d.Name]
			if !present {
				row = ListRow{Name: d.Name, Type: d.Type, Source: d.Source, Parts: slices.Clone(v.Parts), State: "orphaned"}
			}
			if bound[v.ID] {
				row.BoundVersions++
			}
			if exists && current.ID == v.ID {
				id := v.ID
				row.CurrentVersion, row.CurrentSequence = &id, v.Sequence
				row.Type, row.Source, row.Parts = d.Type, d.Source, slices.Clone(v.Parts)
				if declared, err := findDeclaration(declarations, d.Name); err == nil {
					row.State = "current"
					if declared.Fingerprint != d.Fingerprint {
						row.State = "stale"
					}
				}
			}
			rows[d.Name] = row
		}
		for _, row := range rows {
			result.Secrets = append(result.Secrets, row)
		}
		slices.SortFunc(result.Secrets, func(a, b ListRow) int {
			if a.Name < b.Name {
				return -1
			}
			if a.Name > b.Name {
				return 1
			}
			return 0
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s Service) Show(ctx context.Context, request ShowRequest) (*RevealResult, error) {
	selected, declarations, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	d, err := findDeclaration(declarations, request.Name)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(d.Parts(), request.Part) {
		return nil, storage.Failure("part", "requested part is not applicable to this declared secret source")
	}
	var material secrets.Material
	err = s.access.View(ctx, selected, true, func(session storage.StoreSession, _ storage.Selection) error {
		if d.Source == "file" {
			material, err = s.material.File(ctx, d)
			return err
		}
		if session == nil {
			return storage.Failure("store.uninitialized", "secret store is not initialized")
		}
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		v, exists := currentVersion(snapshot, d.Name)
		if !exists {
			return storage.Failure("input", "declared secret has no current material")
		}
		if v.Declaration.Fingerprint != d.Fingerprint {
			return storage.Failure("source", "stored secret is stale for its current declaration")
		}
		material, err = session.Read(ctx, v.ID)
		return err
	})
	if err != nil {
		material.Clear()
		return nil, err
	}
	if err = s.material.Validate(ctx, d, material); err != nil {
		material.Clear()
		return nil, err
	}
	return &RevealResult{Material: material, Part: request.Part}, nil
}

func (s Service) Delete(ctx context.Context, request DeleteRequest) (*MutationResult, error) {
	selected, _, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	if err = requireActive(selected); err != nil {
		return nil, err
	}
	if !validName(request.Name) {
		return nil, storage.Failure("declaration", "secret name is invalid")
	}
	result := &MutationResult{Context: selected, Name: request.Name}
	err = s.access.Mutate(ctx, selected, func(session storage.StoreSession, _ storage.Selection) error {
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		if _, exists := currentVersion(snapshot, request.Name); !exists {
			result.Unchanged = 1
			return nil
		}
		if !request.SkipConfirmation {
			if err := s.confirm(ctx, "delete secret", request.Name); err != nil {
				return err
			}
		}
		changed, err := session.Delete(ctx, request.Name)
		if changed {
			result.Changed = 1
		} else {
			result.Unchanged = 1
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s Service) confirm(ctx context.Context, action, name string) error {
	if s.confirmer == nil {
		return storage.Failure("store.conflict", "secret mutation requires confirmation; use --yes after review")
	}
	if err := s.confirmer.Confirm(ctx, action, name); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return storage.Failure("store.conflict", "secret mutation was not confirmed")
	}
	return nil
}

func requireActive(selected storage.Context) error {
	if selected.Mode != "ready" {
		return storage.Failure("store.conflict", "secret mutation requires a ready context")
	}
	return nil
}

func currentVersion(snapshot storage.Snapshot, name string) (storage.Version, bool) {
	for _, current := range snapshot.Current {
		if current.Name == name {
			for _, version := range snapshot.Versions {
				if version.ID == current.Version {
					return version, true
				}
			}
		}
	}
	return storage.Version{}, false
}

func equalMaterial(a, b secrets.Material) bool {
	if !slices.Equal(a.Parts(), b.Parts()) {
		return false
	}
	equal := 1
	for _, p := range a.Parts() {
		x, _ := a.Part(p)
		y, _ := b.Part(p)
		equal &= subtle.ConstantTimeCompare(x, y)
		clear(x)
		clear(y)
	}
	return equal == 1
}
