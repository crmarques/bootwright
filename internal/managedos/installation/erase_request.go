package installation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

const EraseImplementation = "physical-erase-v1"
const eraseRequestVersion = "physical-erase-v3"
const eraseVariable = "bootwright_physical_erase"

const (
	EraseLogicalOverwrite = "logicalOverwrite"
	EraseMetadataClear    = "metadataClear"
)

type EraseTarget struct {
	DeviceName   string `json:"deviceName,omitempty"`
	Method       string `json:"method"`
	SerialNumber string `json:"serialNumber"`
	SizeBytes    int64  `json:"sizeBytes"`
	WWN          string `json:"wwn,omitempty"`
}

func eraseTargetOrder(a, b EraseTarget) int {
	if order := strings.Compare(a.DeviceName, b.DeviceName); order != 0 {
		return order
	}
	return strings.Compare(a.SerialNumber, b.SerialNumber)
}

type EraseRequest struct {
	DeadlineSeconds int           `json:"deadlineSeconds"`
	Install         Request       `json:"install"`
	InstallDigest   string        `json:"installDigest"`
	Network         string        `json:"network"`
	Targets         []EraseTarget `json:"targets"`
	Version         string        `json:"version"`
}

func EraseBlockID(machine string) string {
	return reconciliation.BlockIdentity("physical-erase-", machine)
}

func EraseContentDigest() string {
	digest := sha256.Sum256([]byte(EraseImplementation + ":" + eraseRequestVersion + ":descriptor-diskseq-method-regions-readback-dm-release-by-number-parallel-paced-union-scan-receipt-v1"))
	return hex.EncodeToString(digest[:])
}

func (r EraseRequest) Canonical() ([]byte, error) { return reconciliation.Freeze(r, "physical erase") }

func (r EraseRequest) SecretReferences() []string { return r.Install.SecretReferences() }

func DecodeEraseRequest(data []byte) (EraseRequest, error) {
	return reconciliation.ThawVersion[EraseRequest](data, "physical erase", eraseRequestVersion)
}

func eraseRequest(machine api.Object, install Request) (EraseRequest, error) {
	policy := machine.Spec().Get("hardware", "eraseOnRemoval")
	request := EraseRequest{Version: eraseRequestVersion, Install: install, DeadlineSeconds: 14400}
	installed, err := install.Canonical()
	if err != nil {
		return request, err
	}
	request.InstallDigest, err = reconciliation.RequestDigest(reconciliation.BlockDefinition{Kind: Kind, Object: install.Identity.Object, Implementation: Implementation, ContentDigest: ContentDigest(), Request: installed})
	if err != nil {
		return request, err
	}
	if !install.Target.Physical || install.Private == nil || machine.Spec().Get("os", "provided").Bool() || !eraseMethod(policy.Get("method").Text()) {
		return request, refusal("api.invariant", "physical erase requires a managed physical Anaconda Machine and the logicalOverwrite or metadataClear method", "omit spec.hardware.eraseOnRemoval to retain its disks")
	}
	for _, line := range strings.Split(install.Kickstart, "\n") {
		if strings.HasPrefix(line, "network ") {
			request.Network += line + "\n"
		}
	}
	if request.Network == "" {
		return request, refusal("api.invariant", "physical erase requires its install network declaration", "declare the Machine install network")
	}
	seen := [3]map[string]bool{{}, {}, {}}
	root := substrate.RootDevicePath(machine)
	hardware := install.Target.Hardware
	for _, value := range policy.Get("targets").Items() {
		size, err := strconv.ParseInt(value.Get("sizeBytes").Text(), 10, 64)
		if err != nil {
			return request, refusal("api.value", "physical erase target size must be an exact integer", "set sizeBytes to the disk's exact logical capacity")
		}
		target := EraseTarget{DeviceName: value.Get("deviceName").Text(), Method: policy.Get("method").Text(), WWN: value.Get("wwn").Text(), SerialNumber: value.Get("serialNumber").Text(), SizeBytes: size}
		if value.Has("method") {
			target.Method = value.Get("method").Text()
		}
		if !eraseMethod(target.Method) {
			return request, refusal("api.value", "physical erase target method must be logicalOverwrite or metadataClear", "correct spec.hardware.eraseOnRemoval.targets")
		}
		if value.Has("deviceName") != value.Has("wwn") {
			return request, refusal("api.value", "physical erase target names its disk by deviceName with its wwn, or by serialNumber and sizeBytes alone", "correct spec.hardware.eraseOnRemoval.targets")
		}
		bySerial := !value.Has("deviceName")
		if !bySerial && !api.ValidLexical("device-path", target.DeviceName) || size < 4096 || size > 64*1024*1024*1024*1024 || size%4096 != 0 {
			return request, refusal("api.value", "physical erase target needs a whole device path and a 4096-byte aligned exact capacity up to 64 TiB", "correct spec.hardware.eraseOnRemoval.targets")
		}
		if target.Method == EraseLogicalOverwrite && size > machineref.MaxLogicalOverwriteBytes {
			return request, refusal("api.value", "physical erase logicalOverwrite target "+eraseTargetName(target)+" is larger than 896 GiB, so its write and readback cannot fit the four-hour erase deadline", "declare method metadataClear for this target, or a disk of at most 896 GiB")
		}
		for index, identity := range []string{target.DeviceName, target.WWN, target.SerialNumber} {
			if bySerial && index < 2 {
				continue
			}
			if identity == "" || len(identity) > 256 || strings.TrimSpace(identity) != identity || strings.ContainsAny(identity, "\r\n\x00") || seen[index][identity] {
				return request, refusal("api.value", "physical erase target identities must be nonempty, bounded and unique", "correct spec.hardware.eraseOnRemoval.targets")
			}
			seen[index][identity] = true
		}
		if root == "" && !bySerial && hardware != nil && hardware.SerialNumber != "" && target.SerialNumber == hardware.SerialNumber {
			return request, refusal("api.invariant", "a serial-selected root erase target uses the original serial-only selector", "omit deviceName and wwn for this root target")
		}
		request.Targets = append(request.Targets, target)
	}
	if err := eraseRootRules(request.Targets, root, hardware, ""); err != nil {
		return request, err
	}
	if len(request.Targets) == 0 || len(request.Targets) > machineref.MaxEraseTargets {
		return request, refusal("api.invariant", "physical erase requires 1 to 16 explicit whole-disk targets", "list each disk to erase; an omitted root disk is retained")
	}
	slices.SortFunc(request.Targets, eraseTargetOrder)
	request.Install.Private = &Publication{Path: install.Private.Path + "-erase", URL: install.Private.URL + "-erase", CertificateRef: install.Private.CertificateRef}
	return request, nil
}

func eraseRootTarget(target EraseTarget, root string, hardware *Hardware, recorded string) bool {
	switch {
	case hardware == nil:
		return false
	case target.DeviceName == "":
		return hardware.SerialNumber != "" && target.SerialNumber == hardware.SerialNumber
	case root != "":
		return target.DeviceName == root || hardware.WWN != "" && eraseWWN(target.WWN) == eraseWWN(hardware.WWN)
	}
	return recorded != "" && (eraseWWN(target.WWN) == eraseWWN(recorded) || strings.HasPrefix(recorded, "naa.") && target.DeviceName == substrate.WWNDevicePath("0x"+eraseWWN(recorded)))
}

func eraseRootRules(targets []EraseTarget, root string, hardware *Hardware, recorded string) error {
	for _, target := range targets {
		if !eraseRootTarget(target, root, hardware, recorded) {
			continue
		}
		if root == "" && target.DeviceName != "" {
			return refusal("api.invariant", "physical erase target "+eraseTargetName(target)+" names the WWN "+recorded+" the serial-selected root disk reported at installation, and a serial-selected root erase target uses the original serial-only selector",
				"omit deviceName and wwn for this root target")
		}
		if target.DeviceName != "" && hardware.WWN != "" && eraseWWN(target.WWN) != eraseWWN(hardware.WWN) || hardware.SerialNumber != "" && target.SerialNumber != hardware.SerialNumber ||
			hardware.SizeBytes != 0 && target.SizeBytes != hardware.SizeBytes {
			return refusal("api.invariant", "physical erase's root target names another wwn, serialNumber or sizeBytes than spec.substrate.baremetal.rootDevice declares for the same disk",
				"make the root target of spec.hardware.eraseOnRemoval and spec.substrate.baremetal.rootDevice name the same wwn, serialNumber and sizeBytes")
		}
	}
	return nil
}

func eraseTargetName(target EraseTarget) string {
	if target.DeviceName == "" {
		return "serial:" + target.SerialNumber
	}
	return target.DeviceName
}

func eraseMethod(method string) bool {
	return method == EraseLogicalOverwrite || method == EraseMetadataClear
}

func eraseWWN(declared string) string {
	lowered := strings.ToLower(declared)
	if hex, found := strings.CutPrefix(lowered, "naa."); found {
		return hex
	}
	return strings.TrimPrefix(lowered, "0x")
}

type EraseCoverage struct {
	EraseTarget
	Written  int64 `json:"written"`
	Verified int64 `json:"verified"`
}

type EraseReceipt struct {
	Version         string          `json:"version"`
	State           string          `json:"state"`
	OriginalAttempt string          `json:"originalAttempt"`
	Request         string          `json:"request"`
	UUID            string          `json:"uuid"`
	MACs            []string        `json:"macs"`
	Targets         []EraseCoverage `json:"targets"`
}

type EraseEvidence struct {
	Version  string        `json:"version"`
	Request  string        `json:"request"`
	Prepared bool          `json:"prepared"`
	Removed  bool          `json:"removed"`
	Receipt  *EraseReceipt `json:"receipt,omitempty"`
	Retained bool          `json:"retained,omitempty"`
}

func validateEraseReceipt(receipt *EraseReceipt, request EraseRequest, digest string) error {
	if receipt == nil || receipt.Version != EraseImplementation || receipt.State != "complete" || receipt.Request != digest || receipt.OriginalAttempt == "" || receipt.UUID == "" || len(receipt.Targets) != len(request.Targets) {
		return refusal("lifecycle.state", "physical erase has no complete original-attempt receipt", "continue the same removal to reprove the target and read back every erased byte")
	}
	var macs []string
	for _, nic := range request.Install.Target.Hardware.Interfaces {
		macs = append(macs, strings.ToLower(nic.MACAddress))
	}
	slices.Sort(macs)
	if !slices.Equal(receipt.MACs, macs) {
		return refusal("lifecycle.state", "physical erase receipt has another complete MAC set", "restore the original operation records")
	}
	for index, target := range request.Targets {
		result := receipt.Targets[index]
		covered := EraseCoverageBytes(target)
		if result.EraseTarget != target || covered == 0 || result.Written != covered || result.Verified != covered {
			return refusal("lifecycle.state", "physical erase receipt does not prove every byte its method erases", "continue the same removal")
		}
	}
	return nil
}

func eraseEvidence(data json.RawMessage, request EraseRequest, digest string, removed bool) error {
	proof, err := reconciliation.DecodeEvidence[EraseEvidence](data, 32768, "physical erase")
	if err != nil {
		return err
	}
	if proof.Version != EraseImplementation || proof.Request != digest || proof.Removed != removed || !proof.Prepared || proof.Retained {
		return refusal("lifecycle.state", "physical erase evidence does not prove this operation", "continue the original operation")
	}
	if removed {
		return validateEraseReceipt(proof.Receipt, request, digest)
	}
	return nil
}

// PhysicalEraseProof validates the original frozen policy, inverse and claim UUID.
func PhysicalEraseProof(applied reconciliation.Block, removed lifecycle.BlockEvidence, originalUUID string) (EraseReceipt, bool, error) {
	if applied.Implementation != EraseImplementation || removed.Implementation != EraseImplementation || applied.Kind != Kind || removed.Kind != Kind ||
		removed.Verb != reconciliation.Destroy || removed.State != reconciliation.BlockDone || removed.Block.ID != applied.ID || removed.Object != applied.Object ||
		removed.Block.RequestDigest != applied.RequestDigest || string(removed.Block.Request) != string(applied.Request) {
		return EraseReceipt{}, false, nil
	}
	request, err := DecodeEraseRequest(applied.Request)
	if err != nil {
		return EraseReceipt{}, false, err
	}
	if err := eraseEvidence(removed.Evidence, request, applied.RequestDigest, true); err != nil {
		return EraseReceipt{}, false, err
	}
	proof, err := reconciliation.DecodeEvidence[EraseEvidence](removed.Evidence, 32768, "physical erase")
	if err != nil {
		return EraseReceipt{}, false, err
	}
	if strings.TrimSpace(originalUUID) == "" || !strings.EqualFold(strings.TrimSpace(proof.Receipt.UUID), strings.TrimSpace(originalUUID)) {
		return EraseReceipt{}, false, refusal("lifecycle.state", "physical erase receipt has another original machine UUID", "restore the original apply and removal records")
	}
	return *proof.Receipt, true, nil
}
