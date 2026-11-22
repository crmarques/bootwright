package custody

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// HostPin carries the input and version enrollment compared.
type HostPin struct {
	Value   []byte
	Version string

	selected    secretstore.Context
	declaration secrets.Declaration
}

func (s Service) ReadHostPin(ctx context.Context, name string, selectPin func(api.Catalog) (string, error)) (HostPin, error) {
	selected, catalog, err := s.resolveCatalog(ctx, name)
	if err != nil {
		return HostPin{}, err
	}
	if err := requireActive(selected); err != nil {
		return HostPin{}, err
	}
	ref, err := selectPin(catalog)
	if err != nil {
		return HostPin{}, err
	}
	object, found := catalog.Find(api.Secret, ref)
	if !found {
		return HostPin{}, secrets.Refusal("declaration", "Secret "+ref+" is not declared", selected.Name, ref, secrets.Command(selected.Name, "check"))
	}
	d := declarationOf(object)
	if d.Type != "opaque" || d.Source != "contextStore" {
		return HostPin{}, secrets.Refusal("source", "Secret "+ref+" must be an opaque contextStore Secret for host-key enrollment", selected.Name, ref, "review the Machine's knownHostsRef")
	}

	pin := HostPin{selected: selected, declaration: d}
	err = s.access.View(ctx, selected, true, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		if session == nil {
			return secretstore.Uninitialized(selected.Name)
		}
		snapshot, err := session.Inspect(ctx)
		if err != nil {
			return err
		}
		if version, exists := currentVersion(snapshot, ref); exists {
			pin.Version = version.ID
			if version.Declaration.Fingerprint == d.Fingerprint {
				value, err := session.Read(ctx, version.ID)
				if err != nil {
					return err
				}
				defer value.Clear()
				pin.Value, _ = value.Part(secrets.ValuePart)
			}
		}
		return nil
	})
	return pin, err
}

func (s Service) CheckHostPinWrite(ctx context.Context, pin HostPin) error {
	held := s.readHolds(ctx, pin.selected)
	return s.access.View(ctx, pin.selected, true, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		return checkHostPinVersion(ctx, session, pin, held)
	})
}

func checkHostPinVersion(ctx context.Context, session secretstore.StoreSession, pin HostPin, held holdIndex) error {
	d, selected := pin.declaration, pin.selected
	if session == nil {
		return secretstore.Uninitialized(selected.Name)
	}
	snapshot, err := session.Inspect(ctx)
	if err != nil {
		return err
	}
	current, _ := currentVersion(snapshot, d.Name)
	if current.ID != pin.Version {
		return changedMeanwhile(selected.Name, d.Name, "written")
	}
	return refuseHeld(held, snapshot, selected.Name, d.Name)
}

func (s Service) StoreHostPin(ctx context.Context, pin HostPin, entry string) (string, error) {
	d, selected := pin.declaration, pin.selected
	value := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(entry)})
	defer value.Clear()
	if err := s.material.Validate(ctx, d, value); err != nil {
		return "", err
	}
	held := s.readHolds(ctx, selected)
	var id string
	err := s.access.Mutate(ctx, selected, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		if err := checkHostPinVersion(ctx, session, pin, held); err != nil {
			return err
		}
		versions, err := session.PutBatch(ctx, []secretstore.Put{{Declaration: d, Material: value}})
		if err == nil {
			id = versions[0].ID
		}
		return err
	})
	return id, err
}
