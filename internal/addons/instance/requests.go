package instance

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type Tool struct {
	Compatibility string `json:"compatibility"`
	Kind          string `json:"kind"`
	Version       string `json:"version"`
}
type Request struct {
	Binding             string                    `json:"binding"`
	BudgetSeconds       int64                     `json:"budgetSeconds"`
	Cluster             string                    `json:"cluster"`
	Package             string                    `json:"package"`
	Placement           machine.Placement         `json:"placement"`
	PlacementNamespaces []string                  `json:"placementNamespaces"`
	ProducedInputs      []lifecycle.ProducedInput `json:"producedInputs"`
	Program             json.RawMessage           `json:"program"`
	Secrets             []string                  `json:"secrets"`
	Tool                Tool                      `json:"tool"`
	Version             string                    `json:"version"`
}

func (r Request) SecretReferences() []string {
	return lifecycle.SecretNames(append(slices.Clone(r.Secrets), r.Placement.SecretReferences()...)...)
}
func (r Request) Deadline() time.Duration {
	return time.Duration(r.BudgetSeconds)*time.Second + 10*time.Minute
}
func (r Request) Canonical() ([]byte, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	return reconciliation.Freeze(r, "add-on instance")
}
func DecodeRequest(data []byte) (Request, error) {
	request, err := reconciliation.ThawVersion[Request](data, "add-on instance", requestVersion)
	if err != nil {
		return Request{}, err
	}
	if err := request.validate(); err != nil {
		return Request{}, err
	}
	return request, nil
}
func (r Request) Objects() (clusterobjects.Program, error) {
	p, err := reconciliation.Thaw[clusterobjects.Program](r.Program, "cluster object program")
	if err != nil {
		return p, err
	}
	options := clusterobjects.Options{PlacementNamespaces: slices.Clone(r.PlacementNamespaces)}
	for _, step := range p.Steps {
		if step.Subscribe != nil {
			options.Namespace = step.Subscribe.Object.Namespace
		}
	}
	return clusterobjects.ThawVersion(r.Program, options)
}
func (r Request) validate() error {
	if r.Version != requestVersion || !reconciliation.ValidSegment(r.Cluster) || !reconciliation.ValidSegment(r.Package) || !reconciliation.ValidSegment(r.Binding) || !reconciliation.ValidSegment(BlockID(r.Package, r.Cluster)) {
		return fmt.Errorf("invalid add-on instance identity")
	}
	if r.BudgetSeconds <= 0 || r.BudgetSeconds > int64((5*time.Hour+50*time.Minute)/time.Second) || r.Deadline() > lifecycle.MaxDeadline {
		return fmt.Errorf("invalid add-on instance budget")
	}
	if !r.Placement.Local() || r.Placement.Machine == "" || r.Placement.Address != "" || r.Placement.PrivateKeyRef != "" || r.Placement.KnownHostsRef != "" || r.Placement.Port != 0 || r.Placement.User != "" {
		return fmt.Errorf("add-on instance must run on the controller")
	}
	if r.Tool.Kind != "openshift-clients" || r.Tool.Compatibility != "openshift" || r.Tool.Version == "" {
		return fmt.Errorf("invalid add-on instance client")
	}
	if len(r.ProducedInputs) < 1 || r.ProducedInputs[0] != (lifecycle.ProducedInput{Kind: "OpenShiftCluster", Object: r.Cluster, Output: "kubeconfig"}) {
		return fmt.Errorf("add-on instance must name its cluster kubeconfig")
	}
	if !slices.IsSorted(r.Secrets) || len(slices.Compact(slices.Clone(r.Secrets))) != len(r.Secrets) {
		return fmt.Errorf("invalid add-on Secret references")
	}
	if r.PlacementNamespaces == nil || !slices.IsSorted(r.PlacementNamespaces) || len(slices.Compact(slices.Clone(r.PlacementNamespaces))) != len(r.PlacementNamespaces) || slices.ContainsFunc(r.PlacementNamespaces, func(namespace string) bool { return !clusterobjects.ValidNamespace(namespace) }) {
		return fmt.Errorf("invalid add-on placement namespaces")
	}
	program, err := r.Objects()
	if err != nil {
		return err
	}
	expected := []string{}
	produced := []lifecycle.ProducedInput{r.ProducedInputs[0]}
	for _, step := range program.Steps {
		if step.Place != nil {
			if p := step.Place.Produced; p != nil {
				ref := lifecycle.ProducedInput{Kind: p.Kind, Object: p.Object, Output: p.Output}
				if !slices.Contains(produced, ref) {
					produced = append(produced, ref)
				}
			} else {
				expected = append(expected, step.Place.Secret)
			}
		}
	}
	if !slices.Equal(produced, r.ProducedInputs) {
		return fmt.Errorf("add-on produced inputs differ from placements")
	}
	slices.Sort(expected)
	expected = slices.Compact(expected)
	if !slices.Equal(expected, r.Secrets) {
		return fmt.Errorf("add-on Secret references differ from placements")
	}
	return nil
}
