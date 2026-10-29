package kubevirt

import (
	"context"
	"maps"
	"time"

	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

const (
	lendOperation    = "lend"
	lendVersion      = "substrate-access-kubevirt-v1"
	lendEvidence     = "substrate-access-kubevirt-evidence-v1"
	lendVariable     = "bootwright_substrate_kubevirt_lend"
	lendOutput       = "kubeconfig"
	lendDeadline     = 3 * time.Minute
	lendMinimum      = 10 * time.Minute
	adminKubeconfig  = "produced:host-kubeconfig"
	lentKubeconfig   = "lent:host-kubeconfig"
	clusterKind      = "OpenShiftCluster"
	clusterOutput    = "kubeconfig"
	maxLendExpiresIn = int64(lifecycle.MaxDeadline/time.Second) + int64(lendMinimum/time.Second)
)

type LendRequest struct {
	Cluster           string `json:"cluster"`
	ExpirationSeconds int64  `json:"expirationSeconds"`
	Namespace         string `json:"namespace"`
	ServiceAccount    string `json:"serviceAccount"`
	Version           string `json:"version"`
}

type LendEvidence struct {
	ExpirationSeconds int64  `json:"expirationSeconds"`
	Postcondition     bool   `json:"postcondition"`
	Request           string `json:"request"`
	Server            string `json:"server"`
	Version           string `json:"version"`
}

type Lender struct{ runner Runner }

func NewLender(runner Runner) Lender { return Lender{runner: runner} }

func HostProducedInput(cluster string) lifecycle.ProducedInput {
	return lifecycle.ProducedInput{Kind: clusterKind, Object: cluster, Output: clusterOutput}
}

func (l Lender) LendHostAccess(ctx context.Context, execution lifecycle.Execution, cluster, namespace string, placement machineref.Placement, deadline time.Duration) (secrets.Material, error) {
	if err := ctx.Err(); err != nil {
		return secrets.Material{}, err
	}
	if l.runner == nil || execution.LendProduced == nil {
		return secrets.Material{}, refusal("lifecycle.state", "the access of host cluster "+cluster+" cannot be lent outside an operation that holds its custody", "")
	}
	request := LendRequest{Cluster: cluster, ExpirationSeconds: expiresIn(deadline), Namespace: namespace, ServiceAccount: substrate.HostAccount, Version: lendVersion}
	canonical, err := reconciliation.Freeze(request, "KubeVirt host access")
	if err != nil {
		return secrets.Material{}, err
	}
	admin, err := execution.LendProduced(ctx, HostProducedInput(cluster))
	if err != nil {
		return secrets.Material{}, err
	}
	defer admin.Clear()
	execution.Material = maps.Clone(execution.Material)
	if execution.Material == nil {
		execution.Material = map[string]secrets.Material{}
	}
	execution.Material[adminKubeconfig] = admin
	result, err := l.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: HostImplementation, Operation: lendOperation, Variable: lendVariable,
		Canonical: canonical, Placement: placement, Deadline: lendDeadline,
		Materials: []lifecycle.MaterialFile{{Name: "host-admin-kubeconfig", Part: secrets.ValuePart, Secret: adminKubeconfig, Variable: "kubeconfig"}},
		Outputs:   []lifecycle.OutputFile{{Name: lendOutput, Variable: lendOutput}},
	}))
	var lent secrets.Material
	found := false
	for _, output := range result.Produced {
		if output.Name == lendOutput && !found {
			lent, found = output.Material, true
			continue
		}
		output.Material.Clear()
	}
	if err == nil {
		err = validateLend(result, request, execution.Block.RequestDigest, found)
	}
	if err != nil {
		lent.Clear()
		return secrets.Material{}, err
	}
	return lent, nil
}

func validateLend(result lifecycle.RunResult, request LendRequest, digest string, found bool) error {
	evidence, err := reconciliation.DecodeEvidence[LendEvidence](result.Evidence, maxEvidenceBytes, "KubeVirt host access")
	switch {
	case err != nil:
		return err
	case evidence.Request != digest || evidence.Version != lendEvidence:
		return refusal("lifecycle.state", "the KubeVirt host access evidence names another request", "")
	case !evidence.Postcondition || !found || !origin(evidence.Server):
		return refusal("lifecycle.state", "the access of host cluster "+request.Cluster+" was not lent", "")
	case evidence.ExpirationSeconds < request.ExpirationSeconds:
		return refusal("lifecycle.state", "host cluster "+request.Cluster+" granted a token shorter than the run it is lent to",
			"allow ServiceAccount tokens of at least "+(time.Duration(request.ExpirationSeconds)*time.Second).String()+" on "+request.Cluster+", or apply again")
	}
	return nil
}

func expiresIn(deadline time.Duration) int64 {
	seconds := int64((max(deadline, 0) + lendMinimum) / time.Second)
	return min(max(seconds, int64(lendMinimum/time.Second)), maxLendExpiresIn)
}

func lentAccess(ctx context.Context, lender HostAccess, execution *lifecycle.Execution, hostCluster, kubeconfigRef, namespace string, placement machineref.Placement, deadline time.Duration) ([]lifecycle.MaterialFile, func(), error) {
	if hostCluster == "" {
		return kubeconfig(kubeconfigRef), func() {}, nil
	}
	material, err := lender.LendHostAccess(ctx, *execution, hostCluster, namespace, placement, deadline)
	if err != nil {
		return nil, func() {}, err
	}
	execution.Material = maps.Clone(execution.Material)
	if execution.Material == nil {
		execution.Material = map[string]secrets.Material{}
	}
	execution.Material[lentKubeconfig] = material
	return []lifecycle.MaterialFile{{Name: "host-kubeconfig", Part: secrets.ValuePart, Secret: lentKubeconfig, Variable: "kubeconfig"}}, material.Clear, nil
}

type HostCustody func(ctx context.Context, contextName, cluster string) (secrets.Material, bool, bool, error)

// UtilityExecution lends a run outside any operation only the proved custodied kubeconfig of cluster.
func UtilityExecution(execution lifecycle.Execution, cluster string, read HostCustody) lifecycle.Execution {
	execution.LendProduced = func(ctx context.Context, input lifecycle.ProducedInput) (secrets.Material, error) {
		if read == nil || input != HostProducedInput(cluster) {
			return secrets.Material{}, refusal("lifecycle.state", "a run outside an operation may borrow only the administrator kubeconfig of the host cluster it reaches", "")
		}
		material, found, unproved, err := read(ctx, execution.Context, cluster)
		if err != nil || !found || unproved {
			material.Clear()
			if err != nil {
				return secrets.Material{}, err
			}
			return secrets.Material{}, refusal("access.unavailable",
				"this context holds no proved administrator kubeconfig for OpenShiftCluster/"+cluster+", whose KubeVirt Machines are reached through it",
				"bootwright apply --context "+execution.Context+" completes that cluster's installation")
		}
		return material, nil
	}
	return execution
}
