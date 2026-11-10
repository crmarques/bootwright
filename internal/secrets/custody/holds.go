package custody

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func (s Service) WithHolds(reader HoldReader) *Service {
	s.holds = reader
	return &s
}

type holdIndex struct {
	bindings map[string]Hold
	listed   map[string]bool
	unnamed  bool
}

func (s Service) readHolds(ctx context.Context, selected secretstore.Context) holdIndex {
	index := holdIndex{bindings: map[string]Hold{}}
	if s.holds == nil {
		return index
	}
	index.listed = map[string]bool{}
	err := s.access.View(ctx, selected, false, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		if session == nil {
			return nil
		}
		snapshot, err := session.Inspect(ctx)
		for _, binding := range snapshot.Bindings {
			index.listed[binding.ID] = true
		}
		return err
	})
	if err != nil {
		index.unnamed = true
		return index
	}
	listed, err := s.holds.SecretHolds(ctx, selected.Name)
	switch {
	case errors.Is(err, availability.ErrNotImplemented):
		return index
	case err != nil:
		index.unnamed = true
		return index
	}
	index.unnamed = listed.Registering
	for _, hold := range listed.Bindings {
		known, seen := index.bindings[hold.Binding]
		if seen {
			hold.Secrets = append(slices.Clone(known.Secrets), hold.Secrets...)
			hold.All = hold.All || known.All
			hold.Operation = known.Operation
		}
		index.bindings[hold.Binding] = hold
	}
	return index
}

func (h holdIndex) holder(snapshot secretstore.Snapshot, name string) (string, bool) {
	current, exists := currentVersion(snapshot, name)
	if !exists || current.Declaration.Type == "caBundle" {
		return "", false
	}
	for _, binding := range snapshot.Bindings {
		if !slices.Contains(binding.Versions, current.ID) {
			continue
		}
		hold, named := h.bindings[binding.ID]
		switch {
		case !named && (h.unnamed || h.listed != nil && !h.listed[binding.ID]):
			return "the Secret binding " + binding.ID + ", which an apply still registering may name", true
		case hold.All || slices.Contains(hold.Secrets, name):
			return hold.Operation + " as effect material", true
		}
	}
	return "", false
}

func heldMaterial(contextName, name, holder string) error {
	return secrets.Refusal("store.conflict",
		"Secret "+name+"'s current version is held by "+holder+", and no command reveals a bound version; nothing was written",
		contextName, name,
		"take the context back with bootwright destroy --context "+contextName+", which releases the binding, then repeat the command")
}

func refuseHeld(held holdIndex, snapshot secretstore.Snapshot, contextName, name string) error {
	if holder, refused := held.holder(snapshot, name); refused {
		return heldMaterial(contextName, name, holder)
	}
	return nil
}

func renews(request GenerateRequest, d secrets.Declaration) bool {
	return request.Renew && (request.Name == "" || request.Name == d.Name)
}

func replacements(snapshot secretstore.Snapshot, contextName string, declarations []secrets.Declaration, request GenerateRequest, held holdIndex) (map[string]string, error) {
	replaced := map[string]string{}
	for _, d := range declarations {
		previous, exists := currentVersion(snapshot, d.Name)
		if d.Source != "generated" || !exists || (previous.Declaration.Fingerprint == d.Fingerprint && !renews(request, d)) {
			continue
		}
		if holder, refused := held.holder(snapshot, d.Name); refused {
			if request.Name == d.Name {
				return nil, heldMaterial(contextName, d.Name, holder)
			}
			continue
		}
		replaced[d.Name] = previous.ID
	}
	return replaced, nil
}

func (s Service) confirmRenewal(ctx context.Context, selected secretstore.Context, declarations []secrets.Declaration, request GenerateRequest, held holdIndex) (map[string]string, error) {
	replaced := map[string]string{}
	err := s.access.View(ctx, selected, false, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		if session == nil {
			return nil
		}
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		replaced, err = replacements(snapshot, selected.Name, declarations, request, held)
		return err
	})
	if err != nil || len(replaced) == 0 {
		return replaced, err
	}
	command := secrets.Command(selected.Name, "generate")
	if request.Name != "" {
		command += " --name " + request.Name
	}
	if err := s.confirm(ctx, "renew secret", selected.Name, renewalSubject(replaced), "renewed", command+" --renew --yes"); err != nil {
		return nil, err
	}
	return replaced, nil
}

func renewalSubject(replaced map[string]string) string {
	return strings.Join(slices.Sorted(maps.Keys(replaced)), ", ")
}
