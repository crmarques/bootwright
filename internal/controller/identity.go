package controller

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/crmarques/bootwright/internal/desiredstate"
)

// LinuxInstalledIdentityV1 identifies the canonical installed-host evidence
// tuple. Its name does not qualify an evidence acquisition implementation.
const LinuxInstalledIdentityV1 = "linux-installed-v1"

// InstalledHostIdentity contains confidential evidence for private binding and
// comparison. It must never be formatted into public output. The zero value is
// invalid; construct values with NewInstalledHostIdentity.
//
// This evidence detects changed identifiers. A complete clone retaining every
// identifier is indistinguishable; this value provides no hardware attestation
// or proof that the caller runs outside a container. An evidence provider must
// independently verify acquisition and the current execution boundary.
//
// Boot IDs, namespace inodes and package/kernel versions are intentionally absent
// because they do not identify the installed host across ordinary reboot and
// maintenance. Workspace separately owns the physical identity of stored state.
type InstalledHostIdentity struct {
	provider       string
	machineID      string
	productUUID    string
	filesystemUUID string
}

// NewInstalledHostIdentity accepts only the supported provider and canonical
// lowercase ASCII identifiers. Acquisition adapters must supply the installed
// machine ID, DMI product UUID and stable root-filesystem UUID; this constructor
// validates their representation without inspecting the host. File terminators,
// alternate UUID spellings and placeholder identifiers are not accepted.
func NewInstalledHostIdentity(provider, machineID, productUUID, filesystemUUID string) (InstalledHostIdentity, error) {
	identity := InstalledHostIdentity{provider: provider, machineID: machineID, productUUID: productUUID, filesystemUUID: filesystemUUID}
	if !identity.Valid() {
		return InstalledHostIdentity{}, invalidInstalledIdentity()
	}
	return identity, nil
}

// Provider returns the evidence format identifier.
func (i InstalledHostIdentity) Provider() string { return i.provider }

// MachineID returns confidential evidence for private persistence only.
func (i InstalledHostIdentity) MachineID() string { return i.machineID }

// ProductUUID returns confidential evidence for private persistence only.
func (i InstalledHostIdentity) ProductUUID() string { return i.productUUID }

// FilesystemUUID returns confidential evidence for private persistence only.
func (i InstalledHostIdentity) FilesystemUUID() string { return i.filesystemUUID }

// Valid reports whether the value satisfies its closed evidence representation.
// It does not verify the evidence against a running host.
func (i InstalledHostIdentity) Valid() bool {
	return i.provider == LinuxInstalledIdentityV1 && validInstalledIdentifier(i.machineID, false) &&
		validInstalledIdentifier(i.productUUID, true) && validInstalledIdentifier(i.filesystemUUID, true)
}

// Equal compares complete valid evidence. Invalid values, including two zero
// values, never prove the same installed host.
func (i InstalledHostIdentity) Equal(other InstalledHostIdentity) bool {
	return i.Valid() && other.Valid() && i == other
}

// PrivateDigest returns lowercase SHA-256 for private binding records only. The
// exact preimage is the ASCII domain below, provider, machine ID, product UUID
// and filesystem UUID, separated by one NUL byte, with no trailing separator.
// It remains confidential correlation evidence and must not enter public output.
func (i InstalledHostIdentity) PrivateDigest() (string, error) {
	if !i.Valid() {
		return "", invalidInstalledIdentity()
	}
	data := "bootwright.controller.installed-host\x00" + i.provider + "\x00" + i.machineID + "\x00" + i.productUUID + "\x00" + i.filesystemUUID
	digest := sha256.Sum256([]byte(data))
	return hex.EncodeToString(digest[:]), nil
}

func validInstalledIdentifier(value string, uuid bool) bool {
	expected := 32
	if uuid {
		expected = 36
	}
	if len(value) != expected {
		return false
	}
	allZero, allF := true, true
	for index := range len(value) {
		c := value[index]
		if uuid && (index == 8 || index == 13 || index == 18 || index == 23) {
			if c != '-' {
				return false
			}
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
		allZero = allZero && c == '0'
		allF = allF && c == 'f'
	}
	return !allZero && !allF
}

func invalidInstalledIdentity() error {
	return desiredstate.NewFailure("controller.identity", "installed-host evidence is malformed or its provider is unsupported", "")
}
