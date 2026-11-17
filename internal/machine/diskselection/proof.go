package diskselection

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/canonicaljson"
)

const MaxReportBytes = 48 << 10

var digestText = regexp.MustCompile(`^[0-9a-f]{64}$`)
var uuidText = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var ownerName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Binding is the provenance of one authenticated RAM inventory. The request
// digest binds inventory media, BMC connection and boot transport. Selection
// independently binds policy and storage-owner constraints.
// Nonce is minted once for the durable execution attempt, never at planning.
type Binding struct {
	Attempt       string   `json:"attempt"`
	Context       string   `json:"context"`
	Incarnation   string   `json:"incarnation"`
	Machine       string   `json:"machine"`
	MACs          []string `json:"macs"`
	MediaSHA256   string   `json:"mediaSHA256"`
	Nonce         string   `json:"nonce"`
	Operation     string   `json:"operation"`
	RequestDigest string   `json:"requestDigest"`
	ToolDigest    string   `json:"toolDigest"`
	UUID          string   `json:"uuid"`
}

const SourceLinux = "linux"
const SourceBMC = "bmc"

type Report struct {
	Binding Binding `json:"binding"`
	Disks   []Disk  `json:"disks"`
	Source  string  `json:"source"`
	Version string  `json:"version"`
}

// Proof is derived from validated inventory; it grants no installation effect.
// A later plan freezes this proof and the Selected identity independently.
type Proof struct {
	Inventoried          []string     `json:"inventoried,omitempty"`
	ReportDigest         string       `json:"reportDigest"`
	Selected             DiskIdentity `json:"selected"`
	SelectionFingerprint string       `json:"selectionFingerprint"`
}

func (p Proof) Validate() error {
	if !digestText.MatchString(p.ReportDigest) || !digestText.MatchString(p.SelectionFingerprint) {
		return fmt.Errorf("inventory selection proof digest is invalid")
	}
	if err := p.Selected.Validate(); err != nil {
		return err
	}
	if p.Inventoried == nil {
		return nil
	}
	if len(p.Inventoried) > MaxDisks || !slices.IsSorted(p.Inventoried) || len(slices.Compact(slices.Clone(p.Inventoried))) != len(p.Inventoried) ||
		slices.ContainsFunc(p.Inventoried, func(s string) bool { return !ValidSerial(s) }) || !slices.Contains(p.Inventoried, p.Selected.SerialNumber) {
		return fmt.Errorf("inventory selection proof names an invalid BMC serial set")
	}
	return nil
}

func (b Binding) Validate() error {
	if !ownerName.MatchString(b.Context) || !ownerName.MatchString(b.Machine) || b.Incarnation == "" || len(b.Incarnation) > 128 || strings.ContainsAny(b.Incarnation, "\x00\r\n") {
		return fmt.Errorf("inventory context or machine binding is invalid")
	}
	if !regexp.MustCompile(`^op-[0-9a-f]{32}$`).MatchString(b.Operation) || !regexp.MustCompile(`^attempt-[0-9]{6}$`).MatchString(b.Attempt) {
		return fmt.Errorf("inventory attempt binding is invalid")
	}
	for _, value := range []string{b.Nonce, b.RequestDigest, b.MediaSHA256, b.ToolDigest} {
		if !digestText.MatchString(value) {
			return fmt.Errorf("inventory provenance digest is invalid")
		}
	}
	if !uuidText.MatchString(b.UUID) || strings.ReplaceAll(b.UUID, "-", "") == strings.Repeat("0", 32) {
		return fmt.Errorf("inventory machine UUID is invalid")
	}
	if len(b.MACs) == 0 || len(b.MACs) > 256 || !slices.IsSorted(b.MACs) {
		return fmt.Errorf("inventory MAC set is invalid")
	}
	for i, value := range b.MACs {
		mac, err := net.ParseMAC(value)
		if err != nil || len(mac) != 6 || mac[0]&1 != 0 || mac.String() != value || value == "00:00:00:00:00:00" || (i > 0 && b.MACs[i-1] == value) {
			return fmt.Errorf("inventory MAC set is invalid")
		}
	}
	return nil
}

func DecodeReport(data []byte) (Report, error) {
	var report Report
	if len(data) > MaxReportBytes {
		return Report{}, fmt.Errorf("inventory report exceeds its byte bound")
	}
	if err := canonicaljson.DecodeClosed(data, &report); err != nil {
		return Report{}, fmt.Errorf("inventory report is not a closed canonical object")
	}
	wire, err := reportWire(report)
	if err != nil || !bytes.Equal(data, wire) {
		return Report{}, fmt.Errorf("inventory report has noncanonical fields or spelling")
	}
	if err := report.Validate(); err != nil {
		return Report{}, err
	}
	return report, nil
}

func (r Report) Validate() error {
	if r.Version != Version {
		return fmt.Errorf("inventory report version is unsupported")
	}
	if r.Source != SourceLinux && r.Source != SourceBMC {
		return fmt.Errorf("inventory report source is unsupported")
	}
	if err := r.Binding.Validate(); err != nil {
		return err
	}
	if len(r.Disks) == 0 || len(r.Disks) > MaxDisks {
		return fmt.Errorf("inventory disk count is invalid")
	}
	for _, d := range r.Disks {
		if err := d.validate(); err != nil {
			return err
		}
	}
	return nil
}

func Prove(report Report, expected Binding, policy Policy, protected []DiskIdentity) (Proof, error) {
	if err := expected.Validate(); err != nil {
		return Proof{}, err
	}
	if err := report.Validate(); err != nil {
		return Proof{}, err
	}
	if !reflect.DeepEqual(report.Binding, expected) {
		return Proof{}, fmt.Errorf("inventory report does not match this machine, context and execution attempt")
	}
	if report.Source == SourceBMC {
		if err := bmcAnswers(policy, protected); err != nil {
			return Proof{}, err
		}
	}
	selected, err := Select(policy, report.Disks, protected)
	if err != nil {
		return Proof{}, err
	}
	fingerprint, err := Fingerprint(policy, protected)
	if err != nil {
		return Proof{}, err
	}
	data, err := reportWire(report)
	if err != nil || len(data) > MaxReportBytes {
		return Proof{}, fmt.Errorf("inventory report cannot be frozen within its byte bound")
	}
	hash := sha256.Sum256(data)
	proof := Proof{ReportDigest: hex.EncodeToString(hash[:]), SelectionFingerprint: fingerprint, Selected: selected}
	if report.Source == SourceBMC {
		for _, d := range report.Disks {
			proof.Inventoried = append(proof.Inventoried, d.SerialNumber)
		}
		slices.Sort(proof.Inventoried)
		proof.Inventoried = slices.Compact(proof.Inventoried)
	}
	return proof, proof.Validate()
}

func bmcAnswers(p Policy, protected []DiskIdentity) error {
	unanswerable := func(d DiskIdentity) bool { return d.SerialNumber == "" || d.WWN != "" }
	if p.Match.ControllerPCIAddress != "" || slices.ContainsFunc(p.Exclude, func(d DiskIdentity) bool { return d.SerialNumber == "" }) || slices.ContainsFunc(protected, unanswerable) {
		return fmt.Errorf("the BMC drive inventory reports no WWN or controller PCI address; name exclusions and storage claims by serial number only, or set rootDisk.discover.inventory to boot")
	}
	return nil
}

// reportWire uses sorted object keys like the native canonical_json transport.
// Public inventory strings are bounded printable ASCII without HTML delimiters.
func reportWire(report Report) ([]byte, error) {
	data, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	return json.Marshal(object)
}

// Canonical is the native inventory wire form; alternate collectors use the
// same closed, bounded transport as the built-in RAM collector.
func (r Report) Canonical() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	data, err := reportWire(r)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxReportBytes {
		return nil, fmt.Errorf("inventory report exceeds its byte bound")
	}
	return data, nil
}
