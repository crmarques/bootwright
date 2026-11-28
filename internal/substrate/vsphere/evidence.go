package vsphere

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type Disk struct {
	Backing   string `json:"backing"`
	Datastore string `json:"datastore"`
	Key       int    `json:"key"`
	Name      string `json:"name"`
	UUID      string `json:"uuid"`
}

type Evidence struct {
	Absent              bool   `json:"absent"`
	Answered            bool   `json:"answered"`
	Disks               []Disk `json:"disks"`
	Foreign             bool   `json:"foreign"`
	InstanceUUID        string `json:"instanceUUID"`
	MoRef               string `json:"moRef"`
	Owned               bool   `json:"owned"`
	Owner               string `json:"owner"`
	Postcondition       bool   `json:"postcondition"`
	Retiring            bool   `json:"retiring,omitempty"`
	Power               string `json:"power"`
	Request             string `json:"request"`
	VCenterUUID         string `json:"vcenterUUID"`
	VMHome              string `json:"vmHome"`
	StagingDatastoreRef string `json:"stagingDatastoreRef"`
}

var (
	instanceUUID   = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	vmReference    = regexp.MustCompile(`^vm-[0-9]+$`)
	storeReference = regexp.MustCompile(`^datastore-[0-9]+$`)
	diskUUID       = regexp.MustCompile(`(?i)^[0-9a-f]{32}$`)
)

func decode(data []byte, digest string) (Evidence, error) {
	e, err := reconciliation.DecodeEvidence[Evidence](data, 64<<10, "machine")
	if err != nil {
		return Evidence{}, err
	}
	if e.Request != digest || !e.Answered || e.Foreign || !instanceUUID.MatchString(e.VCenterUUID) {
		return Evidence{}, unproved.Fail("vCenter could not prove the original request and server identity", "restore access to the original vCenter")
	}
	return e, nil
}

func ValidatePresence(data []byte, r Request, digest string) error {
	return validateOwned(data, r, digest, ready)
}

type phase uint8

const (
	creating phase = iota
	ready
	retiring
)

func (e Evidence) phase() phase {
	if e.Retiring {
		return retiring
	}
	if e.Postcondition {
		return ready
	}
	return creating
}

func validateOwned(data []byte, r Request, digest string, expected phase) error {
	e, err := decode(data, digest)
	if err != nil {
		return err
	}
	owner, operation, ok := strings.Cut(e.Owner, ":")
	if !validVMHome(e.VMHome) || !storeReference.MatchString(e.StagingDatastoreRef) {
		return unproved.Fail("vCenter did not identify the retained VM configuration file and staging datastore", "restore the registered VM inventory")
	}
	if e.phase() != expected || expected != ready && e.Power != "poweredOff" || e.Retiring && e.Postcondition || e.Absent || !e.Owned ||
		owner != r.Owner || !ok || operation == "" || !instanceUUID.MatchString(e.InstanceUUID) ||
		!vmReference.MatchString(e.MoRef) || !slices.Contains([]string{"poweredOff", "poweredOn", "suspended"}, e.Power) {
		return unproved.Fail("the VM identity, ownership marker and power state were not proved", "read the vCenter response and restore the original Machine")
	}
	names := map[string]bool{"root": true}
	for _, disk := range r.DataDisks {
		names[disk.Name] = true
	}
	keys := map[int]bool{}
	uuids := map[string]bool{}
	backings := map[string]bool{}
	if len(names) != len(e.Disks) && !e.partialCloneRoot(r) {
		return unproved.Fail("the owned VMDK set is incomplete", "restore the originally created disks")
	}
	for _, disk := range e.Disks {
		id := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(disk.UUID, "-", ""), " ", ""))
		file := disk.Datastore + "/" + disk.Backing
		if !names[disk.Name] || disk.Key <= 0 || keys[disk.Key] || !diskUUID.MatchString(id) || uuids[id] || !storeReference.MatchString(disk.Datastore) ||
			len(disk.Backing) > 4096 || !strings.HasPrefix(disk.Backing, "[") || !strings.Contains(disk.Backing, "] ") || !strings.HasSuffix(disk.Backing, ".vmdk") || strings.IndexFunc(disk.Backing, unicode.IsControl) >= 0 || backings[file] {
			return unproved.Fail("vCenter did not return a distinct owned VMDK identity for every requested disk", "restore the originally recorded VM and disks")
		}
		delete(names, disk.Name)
		keys[disk.Key], uuids[id], backings[file] = true, true, true
	}
	return nil
}

// A full clone has one independent root before its target devices are configured.
func (e Evidence) partialCloneRoot(r Request) bool {
	return r.Template != "" && e.phase() == creating && e.Power == "poweredOff" && len(e.Disks) == 1 && e.Disks[0].Name == "root"
}

func validVMHome(home string) bool {
	return len(home) <= 4096 && strings.HasPrefix(home, "[") && strings.Contains(home, "] ") && strings.HasSuffix(home, ".vmx") && strings.IndexFunc(home, unicode.IsControl) < 0
}

func validateAbsence(data []byte, digest string) error {
	e, err := decode(data, digest)
	if err != nil {
		return err
	}
	if e.VMHome != "" && !validVMHome(e.VMHome) || e.StagingDatastoreRef != "" && !storeReference.MatchString(e.StagingDatastoreRef) || !e.Absent || !e.Postcondition || e.Retiring || e.Owned || e.MoRef != "" || e.InstanceUUID != "" || e.Owner != "" || e.Power != "" || e.Disks == nil || len(e.Disks) != 0 {
		return unproved.Fail("vCenter did not prove the VM and all recorded VMDKs absent", "inspect the original vCenter and its datastore")
	}
	return nil
}

type pin struct {
	Disks               []Disk `json:"disks"`
	StagingDatastoreRef string `json:"stagingDatastoreRef"`
	InstanceUUID        string `json:"instanceUUID"`
	MoRef               string `json:"moRef"`
	Owner               string `json:"owner"`
}

func PinnedIdentity(name string, records []lifecycle.BlockEvidence) (machine.HardwareIdentity, bool, error) {
	for _, record := range records {
		if record.Implementation != Implementation || record.Object != name || record.Verb != reconciliation.Apply || record.State != reconciliation.BlockDone {
			continue
		}
		r, err := DecodeRequest(record.Block.Request)
		if err != nil {
			return machine.HardwareIdentity{}, false, err
		}
		if err := ValidatePresence(record.Evidence, r, record.Block.RequestDigest); err != nil {
			return machine.HardwareIdentity{}, false, err
		}
		e, _ := decode(record.Evidence, record.Block.RequestDigest)
		if r.Identity.Object != name || r.Identity.Block != record.Block.ID || record.Operation == "" || e.Owner != r.Owner+":"+record.Operation {
			return machine.HardwareIdentity{}, false, unproved.Fail("the VM receipt belongs to a different operation or Machine", "restore the original operation record")
		}
		serial, _ := json.Marshal(pin{Disks: e.Disks, StagingDatastoreRef: e.StagingDatastoreRef, MoRef: e.MoRef, InstanceUUID: e.InstanceUUID, Owner: e.Owner})
		return machine.HardwareIdentity{UUID: e.VCenterUUID, Serial: string(serial)}, true, nil
	}
	return machine.HardwareIdentity{}, false, nil
}
