package access

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
)

// AccessLocator names the custody entry that holds one cluster's
// administrator access: the lifecycle block that captured it and the output's
// name.
type AccessLocator func(cluster string) (block, name string)

type Service struct {
	state     EffectiveState
	reader    ProducedReader
	locate    AccessLocator
	selection machine.CurrentSelection
}

func New(state EffectiveState, reader ProducedReader, locate AccessLocator, selection machine.CurrentSelection) Service {
	return Service{state: state, reader: reader, locate: locate, selection: selection}
}

type OCRequest struct {
	ContextName string
	Name        string
	Command     []string
}

func (Service) OC(ctx context.Context, _ OCRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type KubectlRequest struct {
	ContextName string
	Name        string
	Command     []string
}

func (Service) Kubectl(ctx context.Context, _ KubectlRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

// Kubeconfig reveals the administrator kubeconfig the context's custody holds
// for one selected ContainerCluster, and says when that copy's access was
// never proved. The target and its applicability are
// settled from the compiled graph before any custody read, so a name that
// selects nothing, or selects a StorageCluster, reads no credential bytes.
func (s Service) Kubeconfig(ctx context.Context, request KubeconfigRequest) (*KubeconfigResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.state == nil || s.reader == nil || s.locate == nil {
		return nil, availability.ErrNotImplemented
	}
	name, err := machine.SelectedContext(ctx, s.selection, request.ContextName)
	if err != nil {
		return nil, err
	}
	effective, err := s.state.RenderEffective(ctx, compilation.EffectiveRequest{ContextName: name})
	if err != nil {
		return nil, err
	}
	if err := kubeconfigTarget(effective.Effective, name, request.Name); err != nil {
		return nil, err
	}
	block, output := s.locate(request.Name)
	custodied, found, err := s.reader.ReadProduced(ctx, name, block, output)
	if err != nil {
		custodied.Material.Clear()
		return nil, err
	}
	if !found {
		return nil, failure("access.unavailable",
			"this context holds no administrator kubeconfig for ContainerCluster "+request.Name+"; an apply keeps it once it proves the installation complete",
			"bootwright apply --context "+name)
	}
	return &KubeconfigResult{Context: name, Cluster: request.Name, Material: custodied.Material, Unproved: custodied.Unproved}, nil
}

// kubeconfigTarget resolves --name in the selected graph's shared cluster
// namespace. Only a ContainerCluster, which is always OpenShift or OKD, is a
// kubeconfig target.
func kubeconfigTarget(catalog api.Catalog, contextName, cluster string) error {
	discovery := "bootwright render effective --context " + contextName
	if _, ok := catalog.Find(api.ContainerCluster, cluster); ok {
		return nil
	}
	if storage, ok := catalog.Find(api.StorageCluster, cluster); ok {
		management := storage.Spec().Get("management").Text()
		if management == "" {
			management = "managed"
		}
		return failure("cluster.not-applicable",
			"bootwright cluster kubeconfig does not apply to "+cluster+", a "+management+" Ceph StorageCluster; it applies to OpenShift and OKD ContainerClusters only",
			discovery+" lists the clusters this context selects and their kinds")
	}
	return failure("access.target", "the context "+contextName+" selects no ContainerCluster named "+cluster,
		"name a ContainerCluster this context selects; "+discovery+" lists them")
}

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
