package contextfs

import (
	"bytes"
	"encoding/hex"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// controllerFailure is a controller-state refusal and the action that settles
// it, which belongs to the command that met it.
func controllerFailure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}

const setupBoundRemedy = prerequisites.PurgeRemedy + "; when client areas and the current execution bundle fill the host, no command of this build frees that room"

const retainedSourcesRemedy = "no command of this build frees a retained dependency source, because it never retires one; set this build up on another controller host"

// ControllerRecordVersion is the private shared-host evidence format. Version 2
// names the owning context by name in its receipt, bindings and reservations.
const ControllerRecordVersion = 2

const (
	maxControllerState           = 4 << 20
	maxControllerActions         = 128
	maxControllerSources         = 512
	maxControllerRetainedSources = 4096
	maxControllerStages          = 32
	maxControllerReservations    = 256
	maxControllerReservationKeys = 64
)

type controllerHostRecord struct {
	Provider       string `json:"provider"`
	MachineID      string `json:"machineID"`
	ProductUUID    string `json:"productUUID"`
	FilesystemUUID string `json:"filesystemUUID"`
}

type controllerStateRecord struct {
	Version             int                               `json:"version"`
	Host                controllerHostRecord              `json:"host"`
	Receipt             prerequisites.SetupReceipt        `json:"receipt"`
	Bindings            []prerequisites.ControllerBinding `json:"bindings"`
	RetainedSources     []prerequisites.DependencySource  `json:"retainedSources"`
	RetainedDefinitions []prerequisites.Definition        `json:"retainedDefinitions,omitempty"`
	Bundles             []controllerBundleReservation     `json:"bundles"`
	Reservations        []prerequisites.HostReservation   `json:"reservations,omitempty"`
}

type controllerBundleReservation struct {
	ID              string `json:"id"`
	Mode            string `json:"mode"`
	DirectoryDevice uint64 `json:"directoryDevice"`
	DirectoryInode  uint64 `json:"directoryInode"`
}

// controllerRecord takes the retained bundle reservations explicitly, so a
// publication that changes another part of the record cannot silently drop the
// attribution that proves this store owns its bundle directories.
func controllerRecord(value prerequisites.HostState, bundles []controllerBundleReservation) controllerStateRecord {
	retained := slices.Clone(bundles)
	if retained == nil {
		retained = []controllerBundleReservation{}
	}
	return controllerStateRecord{Version: ControllerRecordVersion, Host: controllerHostRecord{value.Host.Provider(), value.Host.MachineID(), value.Host.ProductUUID(), value.Host.FilesystemUUID()}, Receipt: value.Receipt, Bindings: value.Bindings, RetainedSources: value.RetainedSources, RetainedDefinitions: value.RetainedDefinitions, Bundles: retained, Reservations: value.Reservations}
}

func decodeControllerRecord(data []byte) (prerequisites.HostState, []controllerBundleReservation, error) {
	var record controllerStateRecord
	if err := decodeRecord(data, maxControllerState, &record); err != nil {
		return prerequisites.HostState{}, nil, err
	}
	if record.Version != ControllerRecordVersion {
		return prerequisites.HostState{}, nil, state("controller evidence version is unsupported")
	}
	host, err := controller.NewInstalledHostIdentity(record.Host.Provider, record.Host.MachineID, record.Host.ProductUUID, record.Host.FilesystemUUID)
	if err != nil {
		return prerequisites.HostState{}, nil, state("controller host evidence is malformed")
	}
	value := prerequisites.HostState{Host: host, Receipt: record.Receipt, Bindings: record.Bindings, RetainedSources: record.RetainedSources, RetainedDefinitions: record.RetainedDefinitions, Reservations: record.Reservations}
	if err := validateControllerState(value); err != nil {
		return prerequisites.HostState{}, nil, err
	}
	if record.Bundles == nil || len(record.Bundles) > maxControllerBundles {
		return prerequisites.HostState{}, nil, state("controller bundle reservations exceed their bounds")
	}
	previous := ""
	for _, bundle := range record.Bundles {
		// A retiring entry is an area whose removal is intended. It keeps the
		// attribution that proves the directory is this store's own, because
		// that is what its removal is verified against.
		attributed := bundle.Mode == "attributed" || bundle.Mode == "sealed" || bundle.Mode == "retiring"
		if !validControllerDigest(bundle.ID) || bundle.ID <= previous || !attributed && bundle.Mode != "reserved" || bundle.DirectoryInode == 0 && (bundle.DirectoryDevice != 0 || bundle.Mode != "reserved") || bundle.DirectoryInode != 0 && !attributed {
			return prerequisites.HostState{}, nil, state("controller bundle reservation is invalid")
		}
		previous = bundle.ID
	}
	return value, record.Bundles, nil
}

func cloneControllerState(value prerequisites.HostState) prerequisites.HostState {
	value.Bindings = slices.Clone(value.Bindings)
	value.Reservations = slices.Clone(value.Reservations)
	for index := range value.Reservations {
		value.Reservations[index].Keys = slices.Clone(value.Reservations[index].Keys)
	}
	value.RetainedSources = slices.Clone(value.RetainedSources)
	value.RetainedDefinitions = slices.Clone(value.RetainedDefinitions)
	for index := range value.RetainedDefinitions {
		value.RetainedDefinitions[index] = prerequisites.CloneDefinition(value.RetainedDefinitions[index])
	}
	if value.Receipt.Definition != nil {
		definition := prerequisites.CloneDefinition(*value.Receipt.Definition)
		value.Receipt.Definition = &definition
	}
	value.Receipt.Foundation = prerequisites.CloneQualifiedFoundation(value.Receipt.Foundation)
	value.Receipt.Egress.NoProxy = slices.Clone(value.Receipt.Egress.NoProxy)
	value.Receipt.Sources = slices.Clone(value.Receipt.Sources)
	value.Receipt.Actions = slices.Clone(value.Receipt.Actions)
	for index := range value.Receipt.Actions {
		value.Receipt.Actions[index].Request = slices.Clone(value.Receipt.Actions[index].Request)
		value.Receipt.Actions[index].Evidence = slices.Clone(value.Receipt.Actions[index].Evidence)
		value.Receipt.Actions[index].Preparation = slices.Clone(value.Receipt.Actions[index].Preparation)
	}
	return value
}

func validControllerDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func controllerToken(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._+-:/@", c)) {
			return false
		}
	}
	return true
}

func controllerURL(value string, local bool) bool {
	if value == "" || len(value) > maxPath {
		return false
	}
	for _, c := range value {
		if c <= 32 || c >= 127 {
			return false
		}
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return false
	}
	if local && parsed.Scheme == "file" {
		return parsed.Host == "" && canonicalPath(parsed.Path)
	}
	return (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Hostname() != "" && parsed.Host == strings.ToLower(parsed.Host)
}

func validateControllerObject(data []byte) error {
	if len(data) == 0 || len(data) > maxRecord || data[0] != '{' {
		return state("controller action payload exceeds its bounds or is not an object")
	}
	if err := boundedJSON(data, maxContexts); err != nil {
		return err
	}
	switch err := canonicaljson.ProveObject(data); {
	case err == nil:
		return nil
	case errors.Is(err, canonicaljson.ErrMalformed):
		return state("controller action payload is malformed")
	case errors.Is(err, canonicaljson.ErrTrailing):
		return state("controller action payload contains trailing data")
	default:
		return state("controller action payload is not canonical")
	}
}

func validateControllerSources(values []prerequisites.DependencySource, maximum int) error {
	if values == nil || len(values) > maximum {
		return state("controller source count is invalid")
	}
	previous := ""
	for _, value := range values {
		if !controllerToken(value.ID) || value.ID <= previous || !controllerURL(value.URL, true) || !validControllerDigest(value.SHA256) || value.Bytes <= 0 || value.Bytes > 4<<30 {
			return state("controller source identity or acquisition bound is invalid")
		}
		previous = value.ID
	}
	return nil
}

func validateControllerState(value prerequisites.HostState) error {
	if !value.Host.Valid() {
		return state("controller host evidence is invalid")
	}
	r := value.Receipt
	if err := validateControllerReceiptIdentity(r); err != nil {
		return err
	}
	if err := validateControllerSources(r.Sources, maxControllerSources); err != nil {
		return err
	}
	if err := validateControllerSources(value.RetainedSources, maxControllerRetainedSources); err != nil {
		return err
	}
	if len(value.RetainedDefinitions) > maxControllerBundles {
		return state("retained controller resolutions exceed their bound")
	}
	if err := validateControllerReservations(value.Reservations); err != nil {
		return err
	}
	if err := validateRetainedDefinitions(value); err != nil {
		return err
	}
	if err := validateControllerActions(r); err != nil {
		return err
	}
	digest, err := prerequisites.SetupPlanDigest(value.Host, r)
	if err != nil || digest != r.PlanDigest {
		return state("controller receipt plan digest is inconsistent")
	}
	if err := validateControllerBindings(value); err != nil {
		return err
	}
	for _, source := range r.Sources {
		index := slices.IndexFunc(value.RetainedSources, func(item prerequisites.DependencySource) bool { return item.ID == source.ID })
		if index < 0 || value.RetainedSources[index] != source {
			return state("controller receipt source lacks retained dependency evidence")
		}
	}
	return nil
}

func validateControllerReceiptIdentity(r prerequisites.SetupReceipt) error {
	if !identifier(r.ID, "setup-") || !validControllerDigest(r.CatalogDigest) || !validControllerDigest(r.PlanDigest) {
		return state("controller receipt identity is invalid")
	}
	if r.Context.Name == "" {
		if r.Context != (prerequisites.SetupContext{}) {
			return state("baseline setup contains context identity")
		}
	} else if !contextName(r.Context.Name) || !identifier(r.Context.Revision, "rev-") || !contextName(r.Context.Machine) {
		return state("controller receipt context identity is invalid")
	}
	if r.Egress.HTTPProxy != "" && !controllerURL(r.Egress.HTTPProxy, false) || r.Egress.HTTPSProxy != "" && !controllerURL(r.Egress.HTTPSProxy, false) || r.Egress.NoProxy == nil || len(r.Egress.NoProxy) > 128 {
		return state("controller receipt egress is invalid")
	}
	seenBypass := map[string]bool{}
	for _, item := range r.Egress.NoProxy {
		if len(item) == 0 || len(item) > 1024 || seenBypass[item] {
			return state("controller proxy bypass is invalid")
		}
		for _, c := range item {
			if c <= 32 || c >= 127 {
				return state("controller proxy bypass is invalid")
			}
		}
		seenBypass[item] = true
	}
	if r.Egress.HTTPProxy == "" && r.Egress.HTTPSProxy == "" && len(r.Egress.NoProxy) != 0 {
		return state("direct setup contains proxy bypass configuration")
	}
	return nil
}

func validateRetainedDefinitions(value prerequisites.HostState) error {
	seenDefinitions := map[string]bool{}
	for _, definition := range value.RetainedDefinitions {
		if err := prerequisites.ValidateResolvedDefinition(definition); err != nil || seenDefinitions[definition.ResolutionDigest] {
			return state("retained controller resolution is invalid or duplicated")
		}
		seenDefinitions[definition.ResolutionDigest] = true
		for _, source := range definition.Sources {
			if !slices.Contains(value.RetainedSources, source) {
				return state("retained controller resolution lacks its exact sources")
			}
		}
	}
	r := value.Receipt
	if r.Definition != nil && !slices.ContainsFunc(value.RetainedDefinitions, func(item prerequisites.Definition) bool { return prerequisites.SameDefinition(item, *r.Definition) }) {
		return state("controller receipt lacks its retained resolution")
	}
	if r.Foundation != nil && (r.Definition == nil || prerequisites.ValidateQualifiedFoundation(*r.Foundation, r.Definition.Execution) != nil) {
		return state("controller receipt execution foundation is invalid")
	}
	return nil
}

func validateControllerActions(r prerequisites.SetupReceipt) error {
	if r.Actions == nil || len(r.Actions) == 0 || len(r.Actions) > maxControllerActions {
		return state("controller action count is invalid")
	}
	seenActions := map[string]bool{}
	unresolved, failed := false, false
	for _, action := range r.Actions {
		if !controllerToken(action.ID) || seenActions[action.ID] {
			return state("controller action identity is invalid")
		}
		seenActions[action.ID] = true
		if err := validateControllerObject(action.Request); err != nil {
			return err
		}
		if err := validateControllerObject(action.Evidence); err != nil {
			return err
		}
		if len(action.Preparation) != 0 {
			if err := validateControllerObject(action.Preparation); err != nil {
				return err
			}
			if action.Phase == "planned" || bytes.Equal(action.Preparation, []byte("{}")) {
				return state("controller action before-state lacks attributable intent")
			}
		}
		switch action.Phase {
		case "planned", "intent":
			if action.Outcome != "" || !bytes.Equal(action.Evidence, []byte("{}")) {
				return state("unobserved controller action contains an outcome")
			}
			unresolved = unresolved || action.Phase == "intent"
		case "observed":
			switch action.Outcome {
			case "changed", "unchanged", "failed", "canceled":
			case "unknown":
				unresolved = true
			default:
				return state("controller action outcome is invalid")
			}
			if bytes.Equal(action.Evidence, []byte("{}")) {
				return state("observed controller action lacks attributable evidence")
			}
			failed = failed || action.Outcome == "failed"
		default:
			return state("controller action phase is invalid")
		}
		if r.Status == "complete" && (action.Phase != "observed" || action.Outcome != "changed" && action.Outcome != "unchanged") {
			return state("completed setup lacks successful postconditions")
		}
	}
	switch r.Status {
	case "pending", "unknown", "complete":
	case "failed", "canceled":
		if unresolved || r.Status == "failed" && !failed {
			return state("terminal controller receipt retains unresolved effects")
		}
	default:
		return state("controller receipt status is invalid")
	}
	return nil
}

func validateControllerBindings(value prerequisites.HostState) error {
	hostDigest, _ := value.Host.PrivateDigest()
	if value.Bindings == nil || len(value.Bindings) > maxContexts {
		return state("controller binding count is invalid")
	}
	previous := ""
	for _, binding := range value.Bindings {
		if !contextName(binding.Context) || binding.Context <= previous || !contextName(binding.Machine) || binding.HostDigest != hostDigest {
			return state("controller binding identity is invalid")
		}
		previous = binding.Context
	}
	return nil
}

func validateControllerReferences(value prerequisites.HostState, registry contexts.Registry) error {
	for _, binding := range value.Bindings {
		if !slices.ContainsFunc(registry.Contexts, func(record contexts.Record) bool {
			return record.Name == binding.Context && record.Mode != contexts.Initializing
		}) {
			return state("controller binding references an absent context")
		}
	}
	if receipt := value.Receipt; receipt.Incomplete() && receipt.Context.Name != "" {
		if !slices.ContainsFunc(registry.Contexts, func(record contexts.Record) bool {
			return record.Name == receipt.Context.Name && record.Revision == receipt.Context.Revision && record.Mode == contexts.Ready
		}) {
			return state("pending controller setup input is missing or changed")
		}
	}
	return nil
}

func retainControllerSources(before, next prerequisites.HostState) (prerequisites.HostState, error) {
	// Storage owns append-only resolution history. Callers cannot replace or
	// reorder a frozen plan that may still be needed by another context.
	next.RetainedDefinitions = slices.Clone(before.RetainedDefinitions)
	if next.Receipt.Definition != nil {
		definition := prerequisites.CloneDefinition(*next.Receipt.Definition)
		index := slices.IndexFunc(next.RetainedDefinitions, func(item prerequisites.Definition) bool { return item.ResolutionDigest == definition.ResolutionDigest })
		if index < 0 {
			if len(next.RetainedDefinitions) >= maxControllerBundles {
				return prerequisites.HostState{}, controllerFailure("controller.conflict", "this host already retains the "+strconv.Itoa(maxControllerBundles)+" dependency resolutions it may hold, so setup's new resolution has no room", setupBoundRemedy)
			}
			next.RetainedDefinitions = append(next.RetainedDefinitions, definition)
		} else if !prerequisites.SameDefinition(next.RetainedDefinitions[index], definition) {
			return prerequisites.HostState{}, state("setup would replace immutable resolution evidence")
		}
	}
	values := make(map[string]prerequisites.DependencySource, len(before.RetainedSources)+len(next.RetainedSources)+len(next.Receipt.Sources))
	for _, sources := range [][]prerequisites.DependencySource{before.RetainedSources, next.RetainedSources, next.Receipt.Sources} {
		for _, source := range sources {
			if prior, exists := values[source.ID]; exists && prior != source {
				return prerequisites.HostState{}, state("setup would replace a protected dependency source identity")
			}
			values[source.ID] = source
			if len(values) > maxControllerRetainedSources {
				return prerequisites.HostState{}, controllerFailure("controller.conflict", "this host already retains the "+strconv.Itoa(maxControllerRetainedSources)+" dependency source identities it may hold, so this resolution's sources have no room", retainedSourcesRemedy)
			}
		}
	}
	next.RetainedSources = make([]prerequisites.DependencySource, 0, len(values))
	for _, source := range values {
		next.RetainedSources = append(next.RetainedSources, source)
	}
	slices.SortFunc(next.RetainedSources, func(a, b prerequisites.DependencySource) int { return strings.Compare(a.ID, b.ID) })
	return next, nil
}

// validateControllerReservations keeps the stored claim set canonical and one
// key to one context. The wildcard socket relation is enforced where a claim
// is published, so a record an earlier build wrote stays readable and
// releasable.
func validateControllerReservations(values []prerequisites.HostReservation) error {
	if len(values) > maxControllerReservations {
		return state("controller host reservations exceed their bound")
	}
	previous := [3]string{}
	claimed := map[string]string{}
	for _, reservation := range values {
		if !contextName(reservation.Context) || !contextName(reservation.Kind) || !contextName(reservation.Service) {
			return state("controller host reservation identity is invalid")
		}
		current := [3]string{reservation.Context, reservation.Kind, reservation.Service}
		if current[0] < previous[0] || current[0] == previous[0] && (current[1] < previous[1] || current[1] == previous[1] && current[2] <= previous[2]) {
			return state("controller host reservations are unordered or duplicated")
		}
		previous = current
		if len(reservation.Keys) == 0 || len(reservation.Keys) > maxControllerReservationKeys {
			return state("controller host reservation key count is invalid")
		}
		if !slices.IsSorted(reservation.Keys) {
			return state("controller host reservation keys are unordered")
		}
		for _, key := range reservation.Keys {
			if !controllerToken(key) {
				return state("controller host reservation key is invalid")
			}
			// A shared claim records that a resource is still in use rather
			// than who owns it, so several contexts may hold the same key.
			if reservation.Shared {
				continue
			}
			if owner, taken := claimed[key]; taken && owner != reservation.Context {
				return state("controller host reservation key is claimed by two contexts")
			}
			claimed[key] = reservation.Context
		}
		if len(slices.Compact(slices.Clone(reservation.Keys))) != len(reservation.Keys) {
			return state("controller host reservation keys must be unique")
		}
	}
	return nil
}

// maxControllerBundles bounds the bundle namespaces and retained definitions a
// controller record holds. Setup plans room against the same bound,
// prerequisites.MaxRetainedBundles.
const maxControllerBundles = 16

// heldAreas is what a view tells setup about the areas this record holds, so it
// can decide whether publishing a new bundle needs room first.
func heldAreas(bundles []controllerBundleReservation) []prerequisites.HeldArea {
	held := make([]prerequisites.HeldArea, 0, len(bundles))
	for _, bundle := range bundles {
		held = append(held, prerequisites.HeldArea{ID: bundle.ID, Retiring: bundle.Mode == "retiring"})
	}
	return held
}
