package power

import (
	"context"
	"maps"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

const lentKubeconfig = "lent:host-kubeconfig"

func credentialSecret(access Access) string {
	if access.HostCluster != "" {
		return lentKubeconfig
	}
	return access.CredentialsRef
}

func credentialReferences(access Access) []string {
	var references []string
	if access.HostCluster == "" && access.CredentialsRef != "" {
		references = append(references, access.CredentialsRef)
	}
	if access.TrustBundleRef != "" {
		references = append(references, access.TrustBundleRef)
	}
	return references
}

func hostedNamespace(access Access) string {
	namespace, _, _ := strings.Cut(strings.TrimPrefix(access.Endpoint, "kubevirt:"), "/")
	return namespace
}

const runOverhead = 5 * time.Minute

func powerDeadline(arm string) time.Duration {
	bounds, _ := substrate.MachineBoundsFor(arm)
	return bounds.Power + 2*bounds.StateRead + runOverhead
}

func readDeadline(survey ReadSurvey) time.Duration {
	total := runOverhead
	for _, target := range survey.Targets {
		bounds, _ := substrate.MachineBoundsFor(target.Arm)
		total += bounds.StateRead
	}
	return total
}

func (s Service) lend(ctx context.Context, runtime lifecycle.Runtime, access Access, object string, placement machine.Placement,
	digest string, deadline time.Duration) (map[string]secrets.Material, func(), error) {
	if s.access == nil {
		return nil, func() {}, failure("access.unavailable", "the access of host cluster "+access.HostCluster+" cannot be lent by this executable", "")
	}
	lent, err := s.access.LendHostAccess(ctx, HostLend{
		Runtime: runtime, Cluster: access.HostCluster, Namespace: hostedNamespace(access), Machine: object,
		Placement: placement, Digest: digest, Deadline: deadline,
	})
	if err != nil {
		lent.Clear()
		return nil, func() {}, err
	}
	material := maps.Clone(runtime.Material)
	if material == nil {
		material = map[string]secrets.Material{}
	}
	material[lentKubeconfig] = lent
	return material, lent.Clear, nil
}
