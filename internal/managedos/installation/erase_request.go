package installation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const EraseImplementation = "physical-erase-v2"
const eraseVariable = "bootwright_physical_erase"

type EraseTarget struct {
	DeviceName   string `json:"deviceName"`
	SerialNumber string `json:"serialNumber"`
	SizeBytes    int64  `json:"sizeBytes"`
	WWN          string `json:"wwn"`
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
	digest := sha256.Sum256([]byte(EraseImplementation + ":descriptor-diskseq-full-readback-receipt-v1"))
	return hex.EncodeToString(digest[:])
}

func (r EraseRequest) Canonical() ([]byte, error) { return reconciliation.Freeze(r, "physical erase") }

func (r EraseRequest) SecretReferences() []string { return r.Install.SecretReferences() }

func DecodeEraseRequest(data []byte) (EraseRequest, error) {
	return reconciliation.ThawVersion[EraseRequest](data, "physical erase", EraseImplementation)
}

func eraseRequest(machine api.Object, install Request) (EraseRequest, error) {
	policy := machine.Spec().Get("hardware", "eraseOnRemoval")
	request := EraseRequest{Version: EraseImplementation, Install: install, DeadlineSeconds: 14400}
	installed, err := install.Canonical()
	if err != nil {
		return request, err
	}
	request.InstallDigest, err = reconciliation.RequestDigest(reconciliation.BlockDefinition{Kind: Kind, Object: install.Identity.Object, Implementation: Implementation, ContentDigest: ContentDigest(), Request: installed})
	if err != nil {
		return request, err
	}
	if !install.Target.Physical || install.Private == nil || machine.Spec().Get("os", "provided").Bool() || policy.Get("method").Text() != "logicalOverwrite" {
		return request, refusal("api.invariant", "physical erase requires a managed physical Anaconda Machine and logicalOverwrite", "omit spec.hardware.eraseOnRemoval to retain its disks")
	}
	if err := refusedContinuation(install.Identity.Context, install); err != nil {
		return request, err
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
	root := machine.Spec().Get("hardware", "rootDevice", "deviceName").Text()
	rootIncluded := false
	for _, value := range policy.Get("targets").Items() {
		size, err := strconv.ParseInt(value.Get("sizeBytes").Text(), 10, 64)
		if err != nil {
			return request, refusal("api.value", "physical erase target size must be an exact integer", "set sizeBytes to the disk's exact logical capacity")
		}
		target := EraseTarget{DeviceName: value.Get("deviceName").Text(), WWN: value.Get("wwn").Text(), SerialNumber: value.Get("serialNumber").Text(), SizeBytes: size}
		if !api.ValidLexical("device-path", target.DeviceName) || size < 4096 || size > 64*1024*1024*1024*1024 || size%4096 != 0 {
			return request, refusal("api.value", "physical erase target needs a whole device path and a 4096-byte aligned exact capacity up to 64 TiB", "correct spec.hardware.eraseOnRemoval.targets")
		}
		for index, identity := range []string{target.DeviceName, target.WWN, target.SerialNumber} {
			if identity == "" || len(identity) > 256 || strings.TrimSpace(identity) != identity || strings.ContainsAny(identity, "\r\n\x00") || seen[index][identity] {
				return request, refusal("api.value", "physical erase target identities must be nonempty, bounded and unique", "correct spec.hardware.eraseOnRemoval.targets")
			}
			seen[index][identity] = true
		}
		rootIncluded = rootIncluded || target.DeviceName == root
		request.Targets = append(request.Targets, target)
	}
	if !rootIncluded || len(request.Targets) > 16 {
		return request, refusal("api.invariant", "physical erase requires the approved hardware.rootDevice.deviceName among at most 16 explicit targets", "list the root disk and each data disk to erase")
	}
	slices.SortFunc(request.Targets, func(a, b EraseTarget) int { return strings.Compare(a.DeviceName, b.DeviceName) })
	request.Install.Private = &Publication{Path: install.Private.Path + "-erase", URL: install.Private.URL + "-erase", CertificateRef: install.Private.CertificateRef}
	return request, nil
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
		return refusal("lifecycle.state", "physical erase has no complete original-attempt receipt", "continue the same removal to reprove the target and read every logical byte")
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
		if result.EraseTarget != target || result.Written != target.SizeBytes || result.Verified != target.SizeBytes {
			return refusal("lifecycle.state", "physical erase receipt does not prove every target byte", "continue the same removal")
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
