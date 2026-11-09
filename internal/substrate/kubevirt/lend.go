package kubevirt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

const (
	lendOperation   = "lend"
	revokeOperation = lifecycle.RevokeOperation
	lendSecretName  = "bootwright-kubevirt-lend-"
	revokeDeadline  = substrate.KubeVirtLendBound
	lendBound       = "lend-bound-secret"
	lendVersion     = "substrate-access-kubevirt-v1"
	lendEvidence    = "substrate-access-kubevirt-evidence-v1"
	lendVariable    = "bootwright_substrate_kubevirt_lend"
	lendOutput      = "kubeconfig"
	lendDeadline    = substrate.KubeVirtLendBound
	lendMinimum     = 10 * time.Minute
	adminKubeconfig = "produced:host-kubeconfig"
	lentKubeconfig  = "lent:host-kubeconfig"
	clusterKind     = "OpenShiftCluster"
	clusterOutput   = "kubeconfig"
)

type LendRequest struct {
	Cluster           string `json:"cluster"`
	ExpirationSeconds int64  `json:"expirationSeconds"`
	Namespace         string `json:"namespace"`
	Secret            string `json:"secret"`
	ServiceAccount    string `json:"serviceAccount"`
	Sweep             bool   `json:"sweep"`
	Version           string `json:"version"`
}

type LendEvidence struct {
	ExpirationSeconds int64  `json:"expirationSeconds"`
	Postcondition     bool   `json:"postcondition"`
	Request           string `json:"request"`
	Secret            string `json:"secret"`
	Server            string `json:"server"`
	Version           string `json:"version"`
}

type Lender struct{ runner Runner }

func NewLender(runner Runner) Lender { return Lender{runner: runner} }

func HostProducedInput(cluster string) lifecycle.ProducedInput {
	return lifecycle.ProducedInput{Kind: clusterKind, Object: cluster, Output: clusterOutput}
}

// LendHostAccess also returns the revoke its caller runs once the lent run returns.
func (l Lender) LendHostAccess(ctx context.Context, execution lifecycle.Execution, cluster, namespace string, placement machineref.Placement, deadline time.Duration) (secrets.Material, func(), error) {
	if err := ctx.Err(); err != nil {
		return secrets.Material{}, func() {}, err
	}
	if l.runner == nil || execution.LendProduced == nil {
		return secrets.Material{}, func() {}, refusal("lifecycle.state", "the access of host cluster "+cluster+" cannot be lent outside an operation that holds its custody", "")
	}
	request := LendRequest{Cluster: cluster, ExpirationSeconds: expiresIn(deadline), Namespace: namespace, Secret: lendSecret(execution), ServiceAccount: substrate.HostAccount,
		Sweep: lifecycle.Recorded(execution) && !lifecycle.ReadOnlyRun(execution), Version: lendVersion}
	canonical, err := reconciliation.Freeze(request, "KubeVirt host access")
	if err != nil {
		return secrets.Material{}, func() {}, err
	}
	lent, reached, err := l.run(ctx, execution, lendOperation, request, canonical, placement)
	if err != nil {
		if reached {
			l.revoke(ctx, execution, request, canonical, placement)
		}
		return secrets.Material{}, func() {}, err
	}
	var once sync.Once
	return lent, func() {
		once.Do(func() {
			lent.Clear()
			l.revoke(ctx, execution, request, canonical, placement)
		})
	}, nil
}

func (l Lender) run(ctx context.Context, execution lifecycle.Execution, operation string, request LendRequest, canonical []byte, placement machineref.Placement) (secrets.Material, bool, error) {
	admin, err := execution.LendProduced(ctx, HostProducedInput(request.Cluster))
	if err != nil {
		return secrets.Material{}, false, err
	}
	defer admin.Clear()
	execution.Material = maps.Clone(execution.Material)
	if execution.Material == nil {
		execution.Material = map[string]secrets.Material{}
	}
	execution.Material[adminKubeconfig] = admin
	invocation := lifecycle.Invocation{
		Implementation: HostImplementation, Operation: operation, Variable: lendVariable,
		Canonical: canonical, Placement: placement, Deadline: lendDeadline,
		Materials: []lifecycle.MaterialFile{{Name: "host-admin-kubeconfig", Part: secrets.ValuePart, Secret: adminKubeconfig, Variable: "kubeconfig"}},
	}
	if operation == lendOperation {
		invocation.Outputs = []lifecycle.OutputFile{{Name: lendOutput, Variable: lendOutput}}
	}
	result, err := l.runner.Run(ctx, lifecycle.RunFor(execution, invocation))
	reached := !result.NotStarted
	var lent secrets.Material
	found := false
	for _, output := range result.Produced {
		if output.Name == lendOutput && !found && operation == lendOperation {
			lent, found = output.Material, true
			continue
		}
		output.Material.Clear()
	}
	if err == nil {
		err = validateLend(result, request, execution.Block.RequestDigest, operation, found)
	}
	if err != nil {
		lent.Clear()
		return secrets.Material{}, reached, err
	}
	return lent, true, nil
}

func (l Lender) revoke(ctx context.Context, execution lifecycle.Execution, request LendRequest, canonical []byte, placement machineref.Placement) {
	bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), revokeDeadline)
	defer cancel()
	if _, _, err := l.run(bounded, execution, revokeOperation, request, canonical, placement); err != nil {
		lifecycle.ReportWarning(bounded, execution, LendNotRevoked, "the token lent from host cluster "+request.Cluster+" was not revoked: Secret "+request.Namespace+"/"+request.Secret+
			" may remain until a later lend sweeps it, and the token stays valid until it expires "+
			(time.Duration(request.ExpirationSeconds)*time.Second).String()+" after it was minted")
	}
}

const LendNotRevoked = "substrate.lend-not-revoked"

var lends atomic.Uint64

func lendSecret(execution lifecycle.Execution) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%s\x00%d\x00%d", execution.Context, execution.Block.RequestDigest, time.Now().UnixNano(), lends.Add(1)))
	return lendSecretName + hex.EncodeToString(sum[:8])
}

func validateLend(result lifecycle.RunResult, request LendRequest, digest, operation string, found bool) error {
	evidence, err := reconciliation.DecodeEvidence[LendEvidence](result.Evidence, maxEvidenceBytes, "KubeVirt host access")
	switch {
	case err != nil:
		return err
	case evidence.Request != digest || evidence.Version != lendEvidence || evidence.Secret != request.Secret:
		return refusal("lifecycle.state", "the KubeVirt host access evidence names another request", "")
	case operation == revokeOperation:
		if !evidence.Postcondition || !origin(evidence.Server) {
			return refusal("lifecycle.state", "the access lent from host cluster "+request.Cluster+" was not revoked", "")
		}
	case !evidence.Postcondition || !found || !origin(evidence.Server):
		return refusal("lifecycle.state", "the access of host cluster "+request.Cluster+" was not lent", "")
	case evidence.ExpirationSeconds < request.ExpirationSeconds:
		return refusal("lifecycle.state", "host cluster "+request.Cluster+" granted a token shorter than the run it is lent to",
			"allow ServiceAccount tokens of at least "+(time.Duration(request.ExpirationSeconds)*time.Second).String()+" on "+request.Cluster+", or apply again")
	}
	return nil
}

func expiresIn(deadline time.Duration) int64 {
	return int64((max(deadline, 0) + lendMinimum) / time.Second)
}

func lentAccess(ctx context.Context, lender HostAccess, execution *lifecycle.Execution, hostCluster, kubeconfigRef, namespace string, placement machineref.Placement, deadline time.Duration) ([]lifecycle.MaterialFile, func(), error) {
	if hostCluster == "" {
		return kubeconfig(kubeconfigRef), func() {}, nil
	}
	material, revoke, err := lender.LendHostAccess(ctx, *execution, hostCluster, namespace, placement, deadline)
	if err != nil {
		return nil, func() {}, err
	}
	execution.Material = maps.Clone(execution.Material)
	if execution.Material == nil {
		execution.Material = map[string]secrets.Material{}
	}
	execution.Material[lentKubeconfig] = material
	return []lifecycle.MaterialFile{{Name: "host-kubeconfig", Part: secrets.ValuePart, Secret: lentKubeconfig, Variable: "kubeconfig"}}, revoke, nil
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
