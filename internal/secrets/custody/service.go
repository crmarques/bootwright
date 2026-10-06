package custody

import (
	"context"
	"crypto/subtle"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
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

func (s Service) Set(ctx context.Context, request SetRequest) (*MutationResult, error) {
	selected, declarations, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	if err = requireActive(selected); err != nil {
		return nil, err
	}
	d, err := findDeclaration(selected.Name, declarations, request.Name)
	if err != nil {
		return nil, err
	}
	if d.Source != "contextStore" {
		return nil, secrets.Refusal("source", "Secret "+d.Name+" is generated; secret set stores only a contextStore Secret", selected.Name, d.Name, secrets.Remedy(selected.Name, d, false))
	}
	input := request.Input
	input.ContextName = selected.Name
	if err = secrets.CheckInput(selected.Name, d, input); err != nil {
		return nil, err
	}
	result := &MutationResult{Context: selected, Name: request.Name, Parts: d.Parts()}
	if input.UsesStdin() {
		err = s.setFromStdin(ctx, selected, d, input, request.SkipConfirmation, result)
	} else {
		err = s.access.Mutate(ctx, selected, func(session secretstore.StoreSession, _ secretstore.Selection) error {
			snapshot, err := session.Inspect(ctx)
			if err != nil {
				return err
			}
			previous, exists := currentVersion(snapshot, request.Name)
			if exists && !request.SkipConfirmation {
				if err := s.confirm(ctx, "replace secret", selected.Name, request.Name, "replaced", secrets.Remedy(selected.Name, d, false)+" --yes"); err != nil {
					return err
				}
			}
			material, err := s.material.Acquire(ctx, d, input)
			if err != nil {
				return acquisitionRefusal(err, selected.Name, d)
			}
			defer material.Clear()
			return store(ctx, session, d, previous, exists, material, result)
		})
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

// setFromStdin reads standard input with no store lock held, so a value still
// being typed or piped blocks no other command. It decides under a shared read
// whether a replacement needs --yes, and proves that again under the exclusive
// lock before it writes.
func (s Service) setFromStdin(ctx context.Context, selected secretstore.Context, d secrets.Declaration, input secrets.Input, yes bool, result *MutationResult) error {
	existed := false
	err := s.access.View(ctx, selected, false, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		if session == nil {
			return secrets.Refusal("store.uninitialized", "the secret store of context "+selected.Name+" is not initialized", selected.Name, d.Name, secrets.Command(selected.Name, "encryption init"))
		}
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		_, existed = currentVersion(snapshot, d.Name)
		if existed && !yes {
			return secrets.Refusal("input", "stdin replacement requires --yes before reading material", selected.Name, d.Name, "repeat the command with --yes after reviewing that Secret "+d.Name+" is replaced")
		}
		return nil
	})
	if err != nil {
		return err
	}
	material, err := s.material.Acquire(ctx, d, input)
	if err != nil {
		return acquisitionRefusal(err, selected.Name, d)
	}
	defer material.Clear()
	return s.access.Mutate(ctx, selected, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		previous, exists := currentVersion(snapshot, d.Name)
		if exists && !existed && !yes {
			return secrets.Refusal("store.conflict", "Secret "+d.Name+" was stored by another command while its value was read; nothing was written", selected.Name, d.Name,
				"review it with "+secrets.Command(selected.Name, "check")+", then repeat the command with --yes to replace it")
		}
		return store(ctx, session, d, previous, exists, material, result)
	})
}

// store writes acquired material as the Secret's new current version, unless
// its current version holds equal material for this declaration.
func store(ctx context.Context, session secretstore.StoreSession, d secrets.Declaration, previous secretstore.Version, exists bool, material secrets.Material, result *MutationResult) error {
	if exists && d.Current(previous.Declaration.Fingerprint) {
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
	_, err := session.PutBatch(ctx, []secretstore.Put{{Declaration: d, Material: material}})
	if err == nil {
		result.Changed = 1
	}
	return err
}

// acquisitionRefusal names the Secret whose input acquisition refused, with
// the set command as the fallback remedy. A usage refusal already names it.
func acquisitionRefusal(err error, contextName string, d secrets.Declaration) error {
	if diagnostics.IsUsage(err) || len(diagnostics.Of(err)) == 0 {
		return err
	}
	return secrets.Attribute(err, contextName, d.Name, secrets.Remedy(contextName, d, false))
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
		d, err := findDeclaration(selected.Name, declarations, request.Name)
		if err != nil {
			return nil, err
		}
		if d.Source != "generated" {
			return nil, secrets.Refusal("source", "Secret "+d.Name+" is a contextStore Secret; secret generate mints only generated Secrets", selected.Name, d.Name, secrets.Remedy(selected.Name, d, false))
		}
		declarations = []secrets.Declaration{d}
	}
	result := &MutationResult{Context: selected, Name: request.Name}
	err = s.access.Mutate(ctx, selected, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		var writes []secretstore.Put
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
			if exists && d.Current(previous.Declaration.Fingerprint) && !request.Renew {
				result.UnchangedNames = append(result.UnchangedNames, d.Name)
				continue
			}
			if len(writes) >= secrets.MaxVersions {
				return secretstore.Failure("store.limit", "generated selection exceeds the logical version limit")
			}
			material, err := s.material.Generate(ctx, d)
			if err != nil {
				return err
			}
			if material.Size() > secrets.MaxMaterialBytes-materialBytes {
				material.Clear()
				return secretstore.Failure("store.limit", "generated selection exceeds the material byte limit")
			}
			materialBytes += material.Size()
			writes = append(writes, secretstore.Put{Declaration: d, Material: material})
		}
		if len(writes) == 0 {
			return nil
		}
		if _, err = session.PutBatch(ctx, writes); err != nil {
			return err
		}
		for _, write := range writes {
			result.ChangedNames = append(result.ChangedNames, write.Declaration.Name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(result.ChangedNames)
	slices.Sort(result.UnchangedNames)
	result.Changed, result.Unchanged = len(result.ChangedNames), len(result.UnchangedNames)
	return result, nil
}

func (s Service) Check(ctx context.Context, request CheckRequest) (*CheckResult, error) {
	selected, declarations, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	result := &CheckResult{Context: selected, Secrets: []CheckRow{}}
	var found []diagnostics.Diagnostic
	err = s.access.View(ctx, selected, true, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		var snapshot secretstore.Snapshot
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
			var refused error
			if version, exists := currentVersion(snapshot, d.Name); !exists {
				row.Status = "missing"
				refused = missingMaterial(selected.Name, d)
			} else {
				id := version.ID
				row.Version, row.Sequence = &id, version.Sequence
				if !d.Current(version.Declaration.Fingerprint) {
					row.Status = "stale"
					refused = staleMaterial(selected.Name, d)
				} else {
					var err error
					material, err = session.Read(ctx, version.ID)
					if err != nil {
						return err
					}
					if err = s.material.Validate(ctx, d, material); err != nil {
						row.Status = "invalid"
						refused = secrets.Attribute(err, selected.Name, d.Name, secrets.Remedy(selected.Name, d, true))
					}
				}
			}
			material.Clear()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			found = append(found, diagnostics.Of(refused)...)
			result.Secrets = append(result.Secrets, row)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(found) > 0 {
		diagnostics.Sort(found)
		return result, &diagnostics.Failure{Diagnostics: found}
	}
	return result, nil
}

func (s Service) List(ctx context.Context, request ListRequest) (*ListResult, error) {
	selected, declarations, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	result := &ListResult{Context: selected, Secrets: []ListRow{}}
	err = s.access.View(ctx, selected, true, func(session secretstore.StoreSession, _ secretstore.Selection) error {
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
			// Produced material belongs to the lifecycle block that captured
			// it, never to a Secret declaration, so no secret command lists it.
			// A version an earlier build froze from the retired file source
			// belongs only to the binding that still holds it.
			if d.Source == "file" || d.Source == "produced" {
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
				if declared, found := lookupDeclaration(declarations, d.Name); found {
					row.State = "current"
					if !declared.Current(d.Fingerprint) {
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
	d, err := findDeclaration(selected.Name, declarations, request.Name)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(d.Parts(), request.Part) {
		return nil, wrongPart(selected.Name, d, request.Part)
	}
	var material secrets.Material
	err = s.access.View(ctx, selected, true, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		if session == nil {
			return secrets.Refusal("store.uninitialized", "secret store is not initialized", selected.Name, d.Name, secrets.Command(selected.Name, "encryption init"))
		}
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		v, exists := currentVersion(snapshot, d.Name)
		if !exists {
			return missingMaterial(selected.Name, d)
		}
		if !d.Current(v.Declaration.Fingerprint) {
			return staleMaterial(selected.Name, d)
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
		return nil, secrets.Attribute(err, selected.Name, d.Name, secrets.Remedy(selected.Name, d, true))
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
		return nil, secrets.Refusal("declaration", "secret name is invalid", selected.Name, request.Name, secrets.Command(selected.Name, "list"))
	}
	result := &MutationResult{Context: selected, Name: request.Name}
	err = s.access.Mutate(ctx, selected, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		if _, exists := currentVersion(snapshot, request.Name); !exists {
			result.Unchanged = 1
			return nil
		}
		if !request.SkipConfirmation {
			if err := s.confirm(ctx, "delete secret", selected.Name, request.Name, "deleted", secrets.Command(selected.Name, "delete")+" --name "+request.Name+" --yes"); err != nil {
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

func (s Service) confirm(ctx context.Context, action, contextName, name, outcome, command string) error {
	if s.confirmer != nil {
		err := s.confirmer.Confirm(ctx, action, name)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return secrets.Refusal("store.conflict", "Secret "+name+" was not "+outcome+": the change was declined or could not be confirmed at a terminal", contextName, name,
		"review it with "+secrets.Command(contextName, "check")+", then run "+command)
}

func requireActive(selected secretstore.Context) error {
	if selected.Mode != "ready" {
		return secretstore.Failure("store.conflict", "secret mutation requires a ready context")
	}
	return nil
}

func missingMaterial(contextName string, d secrets.Declaration) error {
	return secrets.Refusal("input", "Secret "+d.Name+" has no stored material", contextName, d.Name, secrets.Remedy(contextName, d, false))
}

func staleMaterial(contextName string, d secrets.Declaration) error {
	return secrets.Refusal("source", "Secret "+d.Name+"'s stored material is stale for its declaration", contextName, d.Name, secrets.Remedy(contextName, d, false))
}

func wrongPart(contextName string, d secrets.Declaration, part secrets.Part) error {
	parts := d.Parts()
	names := make([]string, len(parts))
	for i, value := range parts {
		names[i] = string(value)
	}
	remedy := secrets.Command(contextName, "check")
	if len(names) > 0 {
		remedy = secrets.Command(contextName, "show") + " --name " + d.Name + " --part " + names[0]
	}
	return secrets.Refusal("part", "Secret "+d.Name+" ("+d.Type+") has no "+string(part)+" part; its parts are "+strings.Join(names, ", "), contextName, d.Name, remedy)
}

func currentVersion(snapshot secretstore.Snapshot, name string) (secretstore.Version, bool) {
	for _, current := range snapshot.Current {
		if current.Name == name {
			for _, version := range snapshot.Versions {
				if version.ID == current.Version {
					return version, true
				}
			}
		}
	}
	return secretstore.Version{}, false
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
