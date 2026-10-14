//go:build linux && amd64

package contextfs

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

const (
	maxSetupState         = 256 << 10
	maxSetupActions       = 64
	maxSetupPackages      = 128
	maxSetupPackageOwners = 64
	maxSetupPlatformLike  = 16
	unreadableSetupFormat = "an unreadable record"
)

func encodeSetupState(value prerequisites.SetupState) ([]byte, error) {
	value = normalizedSetupState(value)
	if err := validateSetupState(value); err != nil {
		return nil, err
	}
	return encodeRecord(value, maxSetupState)
}

func decodeSetupState(data []byte) (prerequisites.SetupState, string) {
	var value prerequisites.SetupState
	if err := decodeRecord(data, maxSetupState, &value); err == nil && value.Format == prerequisites.SetupStateFormat && validateSetupState(value) == nil {
		return value, ""
	}
	var header struct {
		Format string `json:"format"`
	}
	if json.Unmarshal(data, &header) == nil && header.Format != prerequisites.SetupStateFormat && controllerToken(header.Format) {
		return prerequisites.SetupState{}, header.Format
	}
	return prerequisites.SetupState{}, unreadableSetupFormat
}

func normalizedSetupState(value prerequisites.SetupState) prerequisites.SetupState {
	value.Format = prerequisites.SetupStateFormat
	if value.Pending != nil {
		pending := *value.Pending
		pending.Actions = nonNil(slices.Clone(pending.Actions))
		value.Pending = &pending
	}
	if value.Receipt != nil {
		receipt := *value.Receipt
		receipt.Platform.Like = nonNil(slices.Clone(receipt.Platform.Like))
		receipt.Packages = slices.Clone(receipt.Packages)
		if receipt.Packages == nil {
			receipt.Packages = []prerequisites.ReceiptPackage{}
		}
		for index := range receipt.Packages {
			receipt.Packages[index].RequiredBy = nonNil(slices.Clone(receipt.Packages[index].RequiredBy))
		}
		value.Receipt = &receipt
	}
	return value
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func validateSetupState(value prerequisites.SetupState) error {
	if value.Format != prerequisites.SetupStateFormat {
		return state("setup record format is unsupported")
	}
	if value.Pending != nil {
		if err := validatePendingSetup(*value.Pending); err != nil {
			return err
		}
	}
	if value.Receipt != nil {
		if err := validateHostReceipt(*value.Receipt); err != nil {
			return err
		}
	}
	runtimes := value.Runtimes
	if runtimes.Current != "" && !validControllerDigest(runtimes.Current) || runtimes.Previous != "" && (!validControllerDigest(runtimes.Previous) || runtimes.Previous == runtimes.Current) {
		return state("setup record runtimes are invalid")
	}
	return nil
}

func validatePendingSetup(pending prerequisites.PendingSetup) error {
	if _, valid := setupRunNumber(pending.ID); !valid || !validControllerDigest(pending.PlanDigest) {
		return state("pending setup identity is invalid")
	}
	if pending.Actions == nil || len(pending.Actions) > maxSetupActions || !tokens(pending.Actions) {
		return state("pending setup actions are invalid")
	}
	started, err := time.Parse(time.RFC3339Nano, pending.StartedAt)
	if err != nil || started.UTC().Format(time.RFC3339Nano) != pending.StartedAt {
		return state("pending setup start time is invalid")
	}
	return nil
}

func validateHostReceipt(receipt prerequisites.HostReceipt) error {
	platform := receipt.Platform
	if !controllerToken(platform.ID) || !controllerToken(platform.VersionID) || !controllerToken(platform.Architecture) || !controllerToken(platform.Glibc) || platform.Like == nil || len(platform.Like) > maxSetupPlatformLike || !tokens(platform.Like) {
		return state("setup receipt platform is invalid")
	}
	runtime := receipt.Runtime
	if !validControllerDigest(receipt.Lock) || !validControllerDigest(receipt.Closure) || !validControllerDigest(runtime.Digest) || !canonicalPath(runtime.Interpreter) || !controllerToken(runtime.Python) || !controllerToken(runtime.ABI) || !controllerToken(runtime.AnsibleCore) {
		return state("setup receipt runtime is invalid")
	}
	if receipt.Packages == nil || len(receipt.Packages) > maxSetupPackages {
		return state("setup receipt package count is invalid")
	}
	for _, item := range receipt.Packages {
		_, run := setupRunNumber(item.InstalledBy)
		if !setupText(item.Requirement) || !setupText(item.Provider) || !setupText(item.Repository) || !run && item.InstalledBy != prerequisites.PresentBeforeBootwright {
			return state("setup receipt package is invalid")
		}
		if item.RequiredBy == nil || len(item.RequiredBy) > maxSetupPackageOwners || !tokens(item.RequiredBy) {
			return state("setup receipt package owners are invalid")
		}
	}
	return nil
}

func tokens(values []string) bool {
	for _, value := range values {
		if !controllerToken(value) {
			return false
		}
	}
	return true
}

func setupText(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, c := range []byte(value) {
		if c < 32 || c > 126 {
			return false
		}
	}
	return true
}
