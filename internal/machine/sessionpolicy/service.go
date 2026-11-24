package sessionpolicy

import (
	"context"
	"fmt"
	"slices"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
)

type Principal struct {
	UID    uint32
	Groups []uint32
}

type Service struct {
	Store   Store
	Lookup  func(context.Context, bool, string) (uint32, error)
	Members func(api.Object) ([]string, bool)
}

type Grant struct {
	Name, RemoteUser                   string
	Subjects                           []api.PolicySubject
	MachineRefs, ClusterRefs, Machines []string
	Commands                           [][]string
	Shell                              bool
}

func (s Service) Validate(ctx context.Context, contextName string, catalog api.Catalog, data []byte) error {
	_, err := s.Expand(ctx, contextName, catalog, data)
	return err
}

func (s Service) Expand(ctx context.Context, contextName string, catalog api.Catalog, data []byte) ([]Grant, error) {
	policy, err := Decode(data, contextName)
	if err != nil {
		return nil, invalid(err)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resolved := make(map[api.PolicySubject]uint32)
	grants := make([]Grant, 0, len(policy.Spec.Grants))
	for _, grant := range policy.Spec.Grants {
		machines, err := s.machines(grant.Targets, catalog)
		if err != nil {
			return nil, invalid(err)
		}
		for _, subject := range grant.Subjects {
			if _, err := s.subject(ctx, resolved, subject); err != nil {
				return nil, invalid(fmt.Errorf("grant %s has an unresolved user or group", grant.Name))
			}
		}
		grants = append(grants, expanded(grant, grant.Subjects, machines))
	}
	return grants, ctx.Err()
}

func (s Service) Installed(ctx context.Context, contextName string) (*api.SessionAccessPolicy, []byte, error) {
	data, err := s.Store.ReadSessionPolicy(ctx, contextName)
	if err != nil {
		return nil, nil, err
	}
	if len(data) == 0 {
		return nil, nil, nil
	}
	policy, err := Decode(data, contextName)
	if err != nil {
		return nil, nil, diagnostics.NewFailureWithRemediation("access.policy", "the installed session access policy of context "+contextName+" is invalid: "+err.Error(), "",
			"install a corrected policy with bootwright policy set, or remove it with bootwright policy delete")
	}
	return &policy, data, nil
}

func (s Service) Permissions(ctx context.Context, principal Principal, contextName string, catalog api.Catalog) ([]Grant, error) {
	data, err := s.Store.ReadSessionPolicy(ctx, contextName)
	if err != nil {
		return nil, denied(contextName, "active policy could not be read")
	}
	if len(data) == 0 {
		return []Grant{}, nil
	}
	policy, err := Decode(data, contextName)
	if err != nil {
		return nil, denied(contextName, "active policy is invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	grants := []Grant{}
	resolved := make(map[api.PolicySubject]uint32)
	for _, grant := range policy.Spec.Grants {
		var matched []api.PolicySubject
		for _, subject := range grant.Subjects {
			id, err := s.subject(ctx, resolved, subject)
			if err != nil {
				return nil, denied(contextName, "a policy subject could not be verified")
			}
			if subject.User != "" && id == principal.UID || subject.Group != "" && slices.Contains(principal.Groups, id) {
				matched = append(matched, subject)
			}
		}
		if len(matched) == 0 {
			continue
		}
		machines, err := s.machines(grant.Targets, catalog)
		if err != nil {
			return nil, denied(contextName, err.Error())
		}
		grants = append(grants, expanded(grant, matched, machines))
	}
	return grants, nil
}

func expanded(grant api.SessionGrant, subjects []api.PolicySubject, machines []string) Grant {
	entry := Grant{Name: grant.Name, Subjects: subjects, RemoteUser: grant.RemoteUser, MachineRefs: grant.Targets.MachineRefs,
		ClusterRefs: grant.Targets.ClusterRefs, Machines: machines, Shell: grant.Rsh != nil}
	if grant.Exec != nil {
		entry.Commands = grant.Exec.Commands
	}
	return entry
}

func (s Service) Authorize(ctx context.Context, principal Principal, contextName string, catalog api.Catalog, targets []machine.SessionTarget, argv []string) error {
	grants, err := s.Permissions(ctx, principal, contextName, catalog)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if !slices.ContainsFunc(grants, func(grant Grant) bool {
			return grant.RemoteUser == target.RemoteUser && slices.Contains(grant.Machines, target.Machine) &&
				(len(argv) == 0 && grant.Shell || slices.ContainsFunc(grant.Commands, func(command []string) bool { return slices.Equal(command, argv) }))
		}) {
			return denied(contextName, "no grant matches Machine "+target.Machine+" and its remote account for this operation")
		}
	}
	return ctx.Err()
}

func (s Service) subject(ctx context.Context, resolved map[api.PolicySubject]uint32, subject api.PolicySubject) (uint32, error) {
	if id, ok := resolved[subject]; ok {
		return id, nil
	}
	name := subject.User
	if subject.Group != "" {
		name = subject.Group
	}
	id, err := s.Lookup(ctx, subject.Group != "", name)
	if err == nil {
		resolved[subject] = id
	}
	return id, err
}

func (s Service) machines(targets api.SessionTargets, catalog api.Catalog) ([]string, error) {
	machines := make(map[string]bool)
	add := func(name string) error {
		if _, found := catalog.Find(api.Machine, name); !found {
			return fmt.Errorf("target Machine %s is absent from the context", name)
		}
		machines[name] = true
		return nil
	}
	for _, name := range targets.MachineRefs {
		if err := add(name); err != nil {
			return nil, err
		}
	}
	for _, name := range targets.ClusterRefs {
		cluster, found := catalog.FindInCategory(api.ContainerClusterCategory, name)
		if !found {
			cluster, found = catalog.FindInCategory(api.StorageClusterCategory, name)
		}
		if !found || cluster.Spec().Get("management").Text() == "external" {
			return nil, fmt.Errorf("target cluster %s has no declared session roster in the context", name)
		}
		membership, ok := s.Members(cluster)
		if !ok || len(membership) == 0 {
			return nil, fmt.Errorf("target cluster %s has no declared session roster in the context", name)
		}
		for _, name := range membership {
			if err := add(name); err != nil {
				return nil, err
			}
		}
	}
	names := make([]string, 0, len(machines))
	for name := range machines {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}

func invalid(err error) error {
	return diagnostics.NewFailureWithRemediation("access.policy", err.Error(), "", "correct the policy and run bootwright policy set again, or check it with --check")
}

func denied(contextName, reason string) error {
	return diagnostics.NewFailureWithRemediation("access.denied", "context "+contextName+": "+reason, "", "review bootwright policy permissions or contact the context administrator")
}

func UnverifiedCaller() error {
	return diagnostics.NewFailure("access.denied", "the invoking account cannot be verified", "")
}
