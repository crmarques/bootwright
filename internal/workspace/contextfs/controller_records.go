package contextfs

import (
	"encoding/hex"
	"net/url"
	"slices"
	"strings"

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

const toolSourcesRemedy = "no command of this build frees a retained tool source, because it never retires one; run this context's controller stage on another controller host"

// ControllerRecordVersion is the private shared-host evidence format. Version 3
// holds host identity, bindings, tool sources and areas, and reservations; the
// setup receipt lives in the setup subtree (D152).
const ControllerRecordVersion = 3

const (
	maxControllerState           = 4 << 20
	maxControllerToolSources     = 4096
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
	Version      int                               `json:"version"`
	Host         controllerHostRecord              `json:"host"`
	Bindings     []prerequisites.ControllerBinding `json:"bindings"`
	ToolSources  []prerequisites.DependencySource  `json:"toolSources"`
	ToolAreas    []controllerBundleReservation     `json:"toolAreas"`
	Reservations []prerequisites.HostReservation   `json:"reservations,omitempty"`
}

type controllerBundleReservation struct {
	ID              string `json:"id"`
	Mode            string `json:"mode"`
	DirectoryDevice uint64 `json:"directoryDevice"`
	DirectoryInode  uint64 `json:"directoryInode"`
}

// controllerRecord takes the tool area reservations explicitly, so a
// publication that changes another part of the record cannot silently drop the
// attribution that proves this store owns its area directories.
func controllerRecord(value prerequisites.HostState, areas []controllerBundleReservation) controllerStateRecord {
	held := slices.Clone(areas)
	if held == nil {
		held = []controllerBundleReservation{}
	}
	sources := slices.Clone(value.ToolSources)
	if sources == nil {
		sources = []prerequisites.DependencySource{}
	}
	bindings := slices.Clone(value.Bindings)
	if bindings == nil {
		bindings = []prerequisites.ControllerBinding{}
	}
	return controllerStateRecord{Version: ControllerRecordVersion, Host: controllerHostRecord{value.Host.Provider(), value.Host.MachineID(), value.Host.ProductUUID(), value.Host.FilesystemUUID()}, Bindings: bindings, ToolSources: sources, ToolAreas: held, Reservations: value.Reservations}
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
	if record.ToolAreas == nil || len(record.ToolAreas) > maxControllerBundles {
		return prerequisites.HostState{}, nil, state("controller tool areas exceed their bounds")
	}
	previous := ""
	for _, area := range record.ToolAreas {
		attributed := area.Mode == "attributed" || area.Mode == "sealed" || area.Mode == retiringArea
		if !validControllerDigest(area.ID) || area.ID <= previous || !attributed && area.Mode != "reserved" || area.DirectoryInode == 0 && (area.DirectoryDevice != 0 || attributed) || area.DirectoryInode != 0 && !attributed {
			return prerequisites.HostState{}, nil, state("controller tool area reservation is invalid")
		}
		previous = area.ID
	}
	value := prerequisites.HostState{Host: host, Bindings: record.Bindings, ToolSources: record.ToolSources, Reservations: record.Reservations}
	if err := validateControllerState(value, record.ToolAreas); err != nil {
		return prerequisites.HostState{}, nil, err
	}
	return value, record.ToolAreas, nil
}

func cloneControllerState(value prerequisites.HostState) prerequisites.HostState {
	value.Bindings = slices.Clone(value.Bindings)
	value.ToolSources = slices.Clone(value.ToolSources)
	value.Reservations = slices.Clone(value.Reservations)
	for index := range value.Reservations {
		value.Reservations[index].Keys = slices.Clone(value.Reservations[index].Keys)
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

func validateControllerState(value prerequisites.HostState, areas []controllerBundleReservation) error {
	if !value.Host.Valid() {
		return state("controller host evidence is invalid")
	}
	if err := validateControllerSources(value.ToolSources, maxControllerToolSources); err != nil {
		return err
	}
	if err := validateControllerReservations(value.Reservations); err != nil {
		return err
	}
	return validateControllerBindings(value, areas)
}

func validateControllerBindings(value prerequisites.HostState, areas []controllerBundleReservation) error {
	hostDigest, _ := value.Host.PrivateDigest()
	if value.Bindings == nil || len(value.Bindings) > maxContexts {
		return state("controller binding count is invalid")
	}
	previous := ""
	for _, binding := range value.Bindings {
		if !contextName(binding.Context) || binding.Context <= previous || !contextName(binding.Machine) || binding.HostDigest != hostDigest {
			return state("controller binding identity is invalid")
		}
		if binding.Tools != "" && !slices.ContainsFunc(areas, func(area controllerBundleReservation) bool { return area.ID == binding.Tools && area.Mode == "sealed" }) {
			return state("controller binding names a tool area this host does not hold sealed")
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
	return nil
}

// validateControllerReservations keeps the stored claim set canonical and one
// key to one context. The wildcard socket relation is enforced where a claim
// is published.
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

// maxControllerBundles bounds the tool areas a controller record holds.
const maxControllerBundles = 16

// toolAreasName is the controller directory's tool area parent.
const toolAreasName = "tools"

// retiringArea marks an area whose retirement began: its entry outlives its
// directory, so a crash leaves an entry the next retirement finishes rather
// than a directory no record explains.
const retiringArea = "retiring"

// heldAreas is what a view tells the controller stage about the tool areas
// this record holds; an area being retired is no longer held.
func heldAreas(areas []controllerBundleReservation) []prerequisites.HeldArea {
	held := make([]prerequisites.HeldArea, 0, len(areas))
	for _, area := range areas {
		if area.Mode != retiringArea {
			held = append(held, prerequisites.HeldArea{ID: area.ID})
		}
	}
	return held
}
