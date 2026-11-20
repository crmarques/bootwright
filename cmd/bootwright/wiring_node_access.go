package main

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster/openshift"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/machine/inventory"
)

type nodeAccessState struct{ state inventory.EffectiveState }

func (s nodeAccessState) RenderEffective(ctx context.Context, request compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	result, err := s.state.RenderEffective(ctx, request)
	if err != nil {
		return nil, err
	}
	copy := *result
	copy.Effective = openshift.AccessCatalog(result.Effective)
	return &copy, nil
}

// Capacity observations use the lifecycle login of the placement host.
type lifecycleAccessState struct{ effective *compilation.EffectiveResult }

func (s lifecycleAccessState) RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	return s.effective, nil
}

func lifecycleAccess(result *compilation.EffectiveResult, name string) (*compilation.EffectiveResult, bool) {
	objects := result.Effective.Objects()
	for i, object := range objects {
		spec := object.Spec()
		login := spec.Get("access", "lifecycle")
		if object.Kind() != api.Machine || object.Name() != name || !login.Present() {
			continue
		}
		ssh := spec.Get("access", "ssh").With("user", login.Get("user")).
			With("knownHostsRef", login.Get("knownHostsRef")).
			With("auth", api.MapValue().With("privateKeyRef", login.Get("privateKeyRef")))
		objects[i] = object.WithSpec(spec.WithPath(ssh, "access", "ssh"))
		copy := *result
		copy.Effective = api.NewCatalog(objects)
		return &copy, login.Get("privilege").Text() == "sudo"
	}
	return result, false
}
