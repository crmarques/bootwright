package networking

import (
	"encoding/json"
	"fmt"
	"time"

	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
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
	BudgetSeconds  int64                     `json:"budgetSeconds"`
	Cluster        string                    `json:"cluster"`
	Placement      machine.Placement         `json:"placement"`
	ProducedInputs []lifecycle.ProducedInput `json:"producedInputs"`
	Program        json.RawMessage           `json:"program"`
	Tool           Tool                      `json:"tool"`
	Version        string                    `json:"version"`
}

func (r Request) Deadline() time.Duration {
	return time.Duration(r.BudgetSeconds)*time.Second + 10*time.Minute
}
func (r Request) Canonical() ([]byte, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	return reconciliation.Freeze(r, "cluster network")
}
func DecodeRequest(data []byte) (Request, error) {
	r, err := reconciliation.ThawVersion[Request](data, "cluster network", requestVersion)
	if err != nil {
		return Request{}, err
	}
	return r, r.validate()
}
func (r Request) Objects() (co.Program, error) { return co.ThawVersion(r.Program, co.Options{}) }
func (r Request) validate() error {
	if r.Version != requestVersion || !reconciliation.ValidSegment(r.Cluster) || !reconciliation.ValidSegment(BlockID(r.Cluster)) {
		return fmt.Errorf("invalid cluster network identity")
	}
	if r.BudgetSeconds <= 0 || r.Deadline() > lifecycle.MaxDeadline {
		return fmt.Errorf("invalid cluster network budget")
	}
	if !r.Placement.Local() || r.Placement.Machine == "" || r.Placement.Address != "" || r.Placement.PrivateKeyRef != "" || r.Placement.KnownHostsRef != "" || r.Placement.Port != 0 || r.Placement.User != "" {
		return fmt.Errorf("cluster network must run on the controller")
	}
	if r.Tool.Kind != "openshift-clients" || r.Tool.Compatibility != "openshift" || r.Tool.Version == "" {
		return fmt.Errorf("invalid cluster network client")
	}
	if len(r.ProducedInputs) != 1 || r.ProducedInputs[0] != (lifecycle.ProducedInput{Kind: "ContainerCluster", Object: r.Cluster, Output: "kubeconfig"}) {
		return fmt.Errorf("cluster network must consume its cluster kubeconfig")
	}
	program, err := r.Objects()
	if err != nil {
		return err
	}
	for _, step := range program.Steps {
		if step.Place != nil || step.Subscribe != nil || step.ApproveDependent != nil {
			return fmt.Errorf("cluster network program cannot place secrets or install operators")
		}
	}
	return nil
}
