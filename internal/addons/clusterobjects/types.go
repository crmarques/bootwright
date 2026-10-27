package clusterobjects

import (
	"encoding/json"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	Version             = "cluster-objects-v2"
	OwnerAnnotation     = "bootwright.io/owner"
	OwnerDomain         = "bootwright.cluster-objects.owner-v1"
	ObjectDomain        = "bootwright.cluster-objects.object-v1"
	ExactRule           = "exact-v1"
	OperatorManagedRule = "operator-managed-v1"
	DependentExactRule  = "dependent-exact-v1"
	ClockSkew           = 5 * time.Second
	MaxObjects          = 64
	MaxChecks           = 32
	MaxSteps            = 96
)

type Ref struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	Namespaced bool   `json:"namespaced"`
}

type Object struct {
	Ref
	Body api.Value
}

type Phase struct {
	Gate      []Check
	Secrets   []SecretPlacement
	Objects   []Object
	Approvals []DependentApproval
}

type DependentApproval struct {
	Namespace       string `json:"namespace"`
	Subscription    string `json:"subscription"`
	Package         string `json:"package"`
	Channel         string `json:"channel"`
	Source          string `json:"source"`
	SourceNamespace string `json:"sourceNamespace"`
	CSV             string `json:"csv"`
	Creator         Ref    `json:"creator"`
}

type SecretPlacement struct {
	Secret      string            `json:"secret,omitempty"`
	Produced    *ProducedRef      `json:"produced,omitempty"`
	ProducedKey string            `json:"producedKey,omitempty"`
	Namespace   string            `json:"namespace"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Keys        []KeyPart         `json:"keys"`
	Labels      map[string]string `json:"labels,omitempty"`
	Strings     map[string]string `json:"strings,omitempty"`
}

type ProducedRef struct {
	Kind   string `json:"kind"`
	Object string `json:"object"`
	Output string `json:"output"`
}

type KeyPart struct {
	Key  string `json:"key"`
	Part string `json:"part"`
}

type Check struct {
	CSVSucceeded   *CSVCheck       `json:"csvSucceeded,omitempty"`
	Condition      *ConditionCheck `json:"condition,omitempty"`
	ResourceExists *Ref            `json:"resourceExists,omitempty"`
	StatusPhase    *PhaseCheck     `json:"statusPhase,omitempty"`
}

type ConditionCheck struct {
	Ref
	Type   string    `json:"type"`
	Status string    `json:"status"`
	FailOn []Failure `json:"failOn,omitempty"`
}

type Failure struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type PhaseCheck struct {
	Ref
	Path   string   `json:"path"`
	Values []string `json:"values"`
}

type CSVCheck struct {
	Namespace    string `json:"namespace"`
	Subscription string `json:"subscription"`
	CSV          string `json:"csv"`
}

type SubscriptionConfig struct {
	Env          []EnvVar
	NodeSelector map[string]string
	Tolerations  []api.Value
	Resources    api.Value
}

type EnvVar struct{ Name, Value string }

type Approval string

const (
	Exact           Approval = "exact"
	OperatorManaged Approval = "operatorManaged"
)

type Program struct {
	Version string  `json:"version"`
	Steps   []Step  `json:"steps"`
	Checks  []Check `json:"checks"`
}

type Step struct {
	Kind             string             `json:"kind"`
	Create           *ObjectEntry       `json:"create,omitempty"`
	Place            *PlacementEntry    `json:"place,omitempty"`
	CatalogReady     *Ref               `json:"catalogReady,omitempty"`
	Subscribe        *Subscription      `json:"subscribe,omitempty"`
	ApproveDependent *DependentApproval `json:"approveDependent,omitempty"`
	Wait             []Check            `json:"wait,omitempty"`
}

type ObjectEntry struct {
	Ref
	Body          json.RawMessage `json:"body"`
	DesiredDigest string          `json:"desiredDigest"`
}

type PlacementEntry struct {
	SecretPlacement
	DesiredDigest string `json:"desiredDigest"`
}

type Subscription struct {
	Object   ObjectEntry `json:"object"`
	Approval Approval    `json:"approval"`
	CSV      string      `json:"csv"`
}

type Options struct {
	Namespace           string
	PlacementNamespaces []string
}
