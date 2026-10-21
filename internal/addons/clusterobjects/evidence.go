package clusterobjects

import (
	"strings"
	"time"
	"unicode"
)

type Ownership string

const (
	Own       Ownership = "own"
	Foreign   Ownership = "foreign"
	Dependent Ownership = "dependent"
	Absent    Ownership = "absent"
	Unknown   Ownership = "unknown"
)

type Receipt struct {
	Ref
	UID           string `json:"uid"`
	DesiredDigest string `json:"desiredDigest"`
	Disposition   string `json:"disposition"`
}

type InstallPlan struct {
	Name string   `json:"name"`
	UID  string   `json:"uid"`
	CSVs []string `json:"csvs"`
}

type OLMFacts struct {
	Namespace           string      `json:"namespace"`
	Subscription        string      `json:"subscription"`
	InstallPlan         InstallPlan `json:"installPlan"`
	InstalledCSV        string      `json:"installedCSV"`
	CSVVersion          string      `json:"csvVersion"`
	UpgradePending      bool        `json:"upgradePending"`
	MaxOpenShiftVersion string      `json:"maxOpenShiftVersion"`
	CatalogImage        string      `json:"catalogImage"`
	BundleImage         string      `json:"bundleImage"`
}

type DependentFacts struct {
	Namespace    string      `json:"namespace"`
	Subscription string      `json:"subscription"`
	UID          string      `json:"uid"`
	InstallPlan  InstallPlan `json:"installPlan"`
	InstalledCSV string      `json:"installedCSV"`
}

type CheckResult struct {
	Unknown         bool   `json:"unknown,omitempty"`
	Index           int    `json:"index"`
	Satisfied       bool   `json:"satisfied"`
	Stale           bool   `json:"stale"`
	LastObservation string `json:"lastObservation"`
}

type ObjectObservation struct {
	Ref
	Ownership Ownership `json:"ownership"`
	Equal     bool      `json:"equal"`
}

type Evidence struct {
	Receipts     []Receipt           `json:"receipts"`
	OLM          []OLMFacts          `json:"olm"`
	Dependents   []DependentFacts    `json:"dependents"`
	Checks       []CheckResult       `json:"checks"`
	Observations []ObjectObservation `json:"observations"`
}

type Probe struct {
	Ref
	ReadKnown bool
	Present   bool
	UID       string
	OwnerMark string
	CreatedAt time.Time
}

func EvaluateOwnership(probe Probe, receipt *Receipt, operation, block string, registered time.Time) Ownership {
	if !probe.ReadKnown {
		return Unknown
	}
	if !probe.Present {
		return Absent
	}
	if probe.UID == "" {
		return Unknown
	}
	if receipt != nil {
		if identity(probe.Ref) == identity(receipt.Ref) && receipt.UID != "" && probe.UID == receipt.UID {
			return Own
		}
		return Foreign
	}
	if operation == "" || block == "" || registered.IsZero() || probe.CreatedAt.IsZero() {
		return Unknown
	}
	if probe.OwnerMark == OwnerMark(operation, block, probe.Ref) && !probe.CreatedAt.Before(registered.Add(-ClockSkew)) {
		return Own
	}
	return Foreign
}

type DependentProbe struct {
	Probe
	Package, Channel, Source, SourceNamespace, StartingCSV, Approval string
}

func EvaluateDependent(probe DependentProbe, expected DependentApproval, creator Probe, creatorOwnership Ownership) Ownership {
	if !probe.ReadKnown {
		return Unknown
	}
	if !probe.Present {
		return Absent
	}
	if !creator.ReadKnown || !creator.Present || creatorOwnership != Own || creator.CreatedAt.IsZero() || probe.CreatedAt.IsZero() || probe.UID == "" {
		return Unknown
	}
	if probe.APIVersion != "operators.coreos.com/v1alpha1" || probe.Kind != "Subscription" || probe.Namespace != expected.Namespace || probe.Name != expected.Subscription || identity(creator.Ref) != identity(expected.Creator) || probe.CreatedAt.Before(creator.CreatedAt) || probe.Package != expected.Package || probe.Channel != expected.Channel || probe.Source != expected.Source || probe.SourceNamespace != expected.SourceNamespace || probe.StartingCSV != expected.CSV || probe.Approval != "Manual" {
		return Foreign
	}
	return Dependent
}

type Realization string

const (
	Completed          Realization = "completed"
	NoEffect           Realization = "noEffect"
	Partial            Realization = "partial"
	UnknownRealization Realization = "unknown"
)

type CSVObservation struct {
	ReadKnown           bool
	Installed, Expected string
}

type Observation struct {
	Objects    []ObjectObservation
	Dependents []Ownership
	Checks     []CheckResult
	CSVs       []CSVObservation
}

func EvaluateObservation(observation Observation) Realization {
	absent, own, ready := 0, 0, true
	for _, object := range observation.Objects {
		switch object.Ownership {
		case Absent:
			absent++
			ready = false
		case Own:
			own++
			ready = ready && object.Equal
		default:
			return UnknownRealization
		}
	}
	for _, dependent := range observation.Dependents {
		switch dependent {
		case Dependent:
		case Absent:
			ready = false
		default:
			return UnknownRealization
		}
	}
	for _, csv := range observation.CSVs {
		if !csv.ReadKnown || (csv.Installed != "" && csv.Installed != csv.Expected) {
			return UnknownRealization
		}
		ready = ready && csv.Installed != ""
	}
	for _, check := range observation.Checks {
		if check.Unknown {
			return UnknownRealization
		}
		ready = ready && check.Satisfied
	}
	if len(observation.Objects) > 0 && absent == len(observation.Objects) && len(observation.Dependents) == 0 {
		return NoEffect
	}
	if ready && own == len(observation.Objects) {
		return Completed
	}
	return Partial
}

func BoundedObservation(value string) string {
	var out strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) {
			continue
		}
		if out.Len()+len(string(r)) > 256 {
			break
		}
		out.WriteRune(r)
	}
	return out.String()
}

func EvaluateEvidence(program Program, evidence Evidence) Realization {
	if ValidateEvidence(evidence) != nil {
		return UnknownRealization
	}
	wanted := map[string]string{}
	expectedCSVs := map[string]string{}
	dependentRefs := map[string]bool{}
	checks := len(program.Checks)
	for _, step := range program.Steps {
		if step.Create != nil {
			wanted[identity(step.Create.Ref)] = step.Create.DesiredDigest
		}
		if step.Place != nil {
			wanted[identity(Ref{APIVersion: "v1", Kind: "Secret", Namespace: step.Place.Namespace, Name: step.Place.Name, Namespaced: true})] = step.Place.DesiredDigest
		}
		if step.Subscribe != nil {
			wanted[identity(step.Subscribe.Object.Ref)] = step.Subscribe.Object.DesiredDigest
			expectedCSVs[step.Subscribe.Object.Namespace+"/"+step.Subscribe.Object.Name] = step.Subscribe.CSV
		}
		if step.ApproveDependent != nil {
			d := step.ApproveDependent
			dependentRefs[d.Namespace+"/"+d.Subscription] = true
			expectedCSVs[d.Namespace+"/"+d.Subscription] = d.CSV
		}
		checks += len(step.Wait)
	}
	if len(evidence.Observations) != len(wanted) || len(evidence.Checks) != checks || len(evidence.Dependents) != len(dependentRefs) {
		return UnknownRealization
	}
	observation := Observation{Objects: evidence.Observations, Checks: evidence.Checks}
	seen := map[string]bool{}
	for _, object := range evidence.Observations {
		key := identity(object.Ref)
		if _, ok := wanted[key]; !ok || seen[key] {
			return UnknownRealization
		}
		seen[key] = true
	}
	receiptKeys := map[string]bool{}
	for _, receipt := range evidence.Receipts {
		key := identity(receipt.Ref)
		digest, ok := wanted[key]
		if !ok || receiptKeys[key] || receipt.UID == "" || receipt.DesiredDigest != digest || (receipt.Disposition != "created" && receipt.Disposition != "unchanged") {
			return UnknownRealization
		}
		receiptKeys[key] = true
	}
	for _, object := range evidence.Observations {
		if object.Ownership == Own && object.Equal && !receiptKeys[identity(object.Ref)] {
			return UnknownRealization
		}
	}
	indices := map[int]bool{}
	for _, check := range evidence.Checks {
		if check.Index < 0 || check.Index >= checks || indices[check.Index] || check.LastObservation != BoundedObservation(check.LastObservation) {
			return UnknownRealization
		}
		indices[check.Index] = true
	}
	csvSeen := map[string]bool{}
	for _, olm := range evidence.OLM {
		key := olm.Namespace + "/" + olm.Subscription
		expected, ok := expectedCSVs[key]
		if !ok || dependentRefs[key] || csvSeen[key] {
			return UnknownRealization
		}
		csvSeen[key] = true
		observation.CSVs = append(observation.CSVs, CSVObservation{ReadKnown: true, Installed: olm.InstalledCSV, Expected: expected})
	}
	for _, dependent := range evidence.Dependents {
		key := dependent.Namespace + "/" + dependent.Subscription
		if !dependentRefs[key] || csvSeen[key] {
			return UnknownRealization
		}
		csvSeen[key] = true
		state := Dependent
		if dependent.UID == "" {
			state = Absent
		}
		observation.Dependents = append(observation.Dependents, state)
		observation.CSVs = append(observation.CSVs, CSVObservation{ReadKnown: true, Installed: dependent.InstalledCSV, Expected: expectedCSVs[key]})
	}
	if len(csvSeen) != len(expectedCSVs) {
		return UnknownRealization
	}
	return EvaluateObservation(observation)
}
