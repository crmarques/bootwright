// Package diskselection owns bounded, deterministic selection from authenticated
// Linux inventory. A policy filters candidates; only an exact disk is a target.
package diskselection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const MaxDisks = 256
const MaxBytes int64 = 64 << 40
const Version = "machine-disk-inventory-v1"

var pci = regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`)
var naa = regexp.MustCompile(`^0x(?:[0-9a-f]{16}|[0-9a-f]{32})$`)
var serial = regexp.MustCompile(`^[A-Za-z0-9._:+/=-]{1,256}$`)

// Match contains fleet policy, not a disk identity. Omission widens a predicate.
type Match struct {
	ControllerPCIAddress string `json:"controllerPCIAddress,omitempty"`
	MaxSizeBytes         int64  `json:"maxSizeBytes,omitempty"`
	MinSizeBytes         int64  `json:"minSizeBytes,omitempty"`
	Model                string `json:"model,omitempty"`
}

// DiskIdentity can exclude or freeze one exact OS-visible whole disk. A serial
// always carries exact capacity, even when a WWN is also known.
type DiskIdentity struct {
	SerialNumber string `json:"serialNumber,omitempty"`
	SizeBytes    int64  `json:"sizeBytes"`
	WWN          string `json:"wwn,omitempty"`
}

type Policy struct {
	Exclude []DiskIdentity `json:"exclude,omitempty"`
	Match   Match          `json:"match,omitempty"`
}

// Disk is one bounded kernel observation. The collector reports ineligible
// whole disks too; an unreadable attribute never becomes a positive predicate.
type Disk struct {
	DiskIdentity
	Model                string `json:"model"`
	ControllerPCIAddress string `json:"controllerPCIAddress"`
	Transport            string `json:"transport"`
	Fixed                bool   `json:"fixed"`
	Writable             bool   `json:"writable"`
	Whole                bool   `json:"whole"`
	Hidden               bool   `json:"hidden"`
}

func validSize(n int64) bool { return n >= 4096 && n <= MaxBytes && n%4096 == 0 }

func (d DiskIdentity) Validate() error {
	if !validSize(d.SizeBytes) || (d.WWN == "" && d.SerialNumber == "") {
		return fmt.Errorf("disk identity requires exact aligned capacity and a stable identifier")
	}
	if d.WWN != "" && (!naa.MatchString(d.WWN) || strings.Trim(d.WWN[2:], "0") == "") {
		return fmt.Errorf("disk WWN is not a nonzero NAA identity")
	}
	if d.SerialNumber != "" && !serial.MatchString(d.SerialNumber) {
		return fmt.Errorf("disk serial is outside the qualified grammar")
	}
	return nil
}

// ValidateProtection permits a WWN-only protected claim: inventory supplies its
// exact capacity, but no missing authored size may weaken a serial selector.
func (d DiskIdentity) ValidateProtection() error {
	if d.SizeBytes == 0 && d.WWN != "" && d.SerialNumber == "" {
		d.SizeBytes = 4096
	}
	return d.Validate()
}

func (p Policy) Validate() error {
	m := p.Match
	if len(m.Model) > 256 || strings.TrimSpace(m.Model) != m.Model || !inventoryText(m.Model) {
		return fmt.Errorf("disk model predicate is invalid")
	}
	if m.ControllerPCIAddress != "" && !pci.MatchString(m.ControllerPCIAddress) {
		return fmt.Errorf("controller PCI address predicate is invalid")
	}
	if (m.MinSizeBytes != 0 && !validSize(m.MinSizeBytes)) || (m.MaxSizeBytes != 0 && !validSize(m.MaxSizeBytes)) || (m.MaxSizeBytes != 0 && m.MinSizeBytes > m.MaxSizeBytes) {
		return fmt.Errorf("disk capacity range is invalid")
	}
	if len(p.Exclude) > MaxDisks {
		return fmt.Errorf("too many disk exclusions")
	}
	for _, d := range p.Exclude {
		if err := d.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (d Disk) validate() error {
	if len(d.Model) > 256 || strings.TrimSpace(d.Model) != d.Model || !inventoryText(d.Model) {
		return fmt.Errorf("inventory model is invalid")
	}
	if d.ControllerPCIAddress != "" && !pci.MatchString(d.ControllerPCIAddress) {
		return fmt.Errorf("inventory PCI address is invalid")
	}
	if !slices.Contains([]string{"sata", "sas", "scsi", "nvme", "usb", "unknown"}, d.Transport) {
		return fmt.Errorf("inventory transport is invalid")
	}
	// Unknown identities may be reported, but can never qualify as targets.
	if d.WWN != "" || d.SerialNumber != "" {
		return d.DiskIdentity.Validate()
	}
	if d.SizeBytes != 0 && !validSize(d.SizeBytes) {
		return fmt.Errorf("inventory capacity is invalid")
	}
	return nil
}

// Select refuses ambiguity before returning a frozen identity. Protected is a
// complete, positively resolved set supplied by storage owners, not a guess
// based on signatures currently present on disk.
func Select(p Policy, disks []Disk, protected []DiskIdentity) (DiskIdentity, error) {
	if err := p.Validate(); err != nil {
		return DiskIdentity{}, err
	}
	if len(disks) == 0 || len(disks) > MaxDisks || len(protected) > MaxDisks {
		return DiskIdentity{}, fmt.Errorf("disk inventory is empty or exceeds its bound")
	}
	for _, d := range protected {
		if err := d.ValidateProtection(); err != nil {
			return DiskIdentity{}, fmt.Errorf("protected disk identity: %w", err)
		}
	}
	for _, d := range disks {
		if err := d.validate(); err != nil {
			return DiskIdentity{}, err
		}
	}
	for _, selected := range protected {
		count := 0
		for _, disk := range disks {
			if !disk.Whole || disk.Hidden {
				continue
			}
			if (selected.SizeBytes == 0 || disk.SizeBytes == selected.SizeBytes) && (selected.WWN == "" || selected.WWN == disk.WWN) && (selected.SerialNumber == "" || selected.SerialNumber == disk.SerialNumber) {
				count++
			}
		}
		if count != 1 {
			return DiskIdentity{}, fmt.Errorf("protected disk cannot be resolved uniquely in the complete inventory")
		}
	}
	var candidates []DiskIdentity
	for _, d := range disks {
		if !d.Fixed || !d.Writable || !d.Whole || d.Hidden || d.Transport == "usb" {
			continue
		}
		if d.Transport == "unknown" || d.DiskIdentity.Validate() != nil {
			return DiskIdentity{}, fmt.Errorf("inventory contains a potentially eligible disk with unknown identity, capacity or transport")
		}
		if excluded(d.DiskIdentity, p.Exclude) || excluded(d.DiskIdentity, protected) {
			continue
		}
		m := p.Match
		if (m.Model != "" && d.Model == "") || (m.ControllerPCIAddress != "" && d.ControllerPCIAddress == "") {
			return DiskIdentity{}, fmt.Errorf("a potentially eligible disk has an unknown selection predicate")
		}
		if (m.Model != "" && d.Model != m.Model) || (m.ControllerPCIAddress != "" && d.ControllerPCIAddress != m.ControllerPCIAddress) || (m.MinSizeBytes != 0 && d.SizeBytes < m.MinSizeBytes) || (m.MaxSizeBytes != 0 && d.SizeBytes > m.MaxSizeBytes) {
			continue
		}
		if excluded(d.DiskIdentity, p.Exclude) || excluded(d.DiskIdentity, protected) {
			continue
		}
		if !unique(d.DiskIdentity, disks) {
			return DiskIdentity{}, fmt.Errorf("eligible disk identity is ambiguous in the complete inventory")
		}
		candidates = append(candidates, d.DiskIdentity)
	}
	if len(candidates) != 1 {
		return DiskIdentity{}, fmt.Errorf("root discovery requires exactly one eligible disk; found %d", len(candidates))
	}
	return candidates[0], nil
}

func excluded(d DiskIdentity, selectors []DiskIdentity) bool {
	for _, s := range selectors {
		// Either shared identifier protects a device even if capacity drifted.
		if (s.WWN != "" && s.WWN == d.WWN) || (s.SerialNumber != "" && s.SerialNumber == d.SerialNumber) {
			return true
		}
	}
	return false
}

func unique(d DiskIdentity, disks []Disk) bool {
	w, s := 0, 0
	for _, other := range disks {
		if other.Hidden || !other.Whole {
			continue
		}
		if d.WWN != "" && other.WWN == d.WWN {
			w++
		}
		if d.SerialNumber != "" && other.SerialNumber == d.SerialNumber {
			s++
		}
	}
	return (d.WWN == "" || w == 1) && (d.SerialNumber == "" || s == 1)
}

// Fingerprint binds selection policy and all protected claims. Canonical
// ordering makes independent authoring order irrelevant to the proof binding.
func Fingerprint(p Policy, protected []DiskIdentity) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	if len(protected) > MaxDisks {
		return "", fmt.Errorf("too many protected disks")
	}
	p.Exclude = slices.Clone(p.Exclude)
	protected = append([]DiskIdentity{}, protected...)
	order := func(a, b DiskIdentity) int {
		aa, _ := json.Marshal(a)
		bb, _ := json.Marshal(b)
		return strings.Compare(string(aa), string(bb))
	}
	for _, d := range protected {
		if err := d.ValidateProtection(); err != nil {
			return "", err
		}
	}
	slices.SortFunc(p.Exclude, order)
	p.Exclude = slices.Compact(p.Exclude)
	slices.SortFunc(protected, order)
	protected = slices.Compact(protected)
	data, err := json.Marshal(struct {
		Version   string         `json:"version"`
		Policy    Policy         `json:"policy"`
		Protected []DiskIdentity `json:"protected"`
	}{Version, p, protected})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func inventoryText(value string) bool {
	for _, r := range value {
		if r < 32 || r > 126 || r == '<' || r == '>' || r == '&' {
			return false
		}
	}
	return true
}
