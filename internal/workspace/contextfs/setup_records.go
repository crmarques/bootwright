//go:build linux && amd64

package contextfs

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

const (
	maxSetupState         = 8 << 20
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
		pending.Requested = nonNil(slices.Clone(pending.Requested))
		pending.Added = addedSet(pending.Added)
		if pending.Transaction != nil {
			transaction := *pending.Transaction
			transaction.Held = nonNil(slices.Clone(transaction.Held))
			pending.Transaction = &transaction
		}
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
		receipt.Added = addedSet(receipt.Added)
		value.Receipt = &receipt
	}
	return value
}

func addedSet(values []prerequisites.InstalledPackage) []prerequisites.InstalledPackage {
	if values == nil {
		return []prerequisites.InstalledPackage{}
	}
	return slices.Clone(values)
}

func validAddedSet(values []prerequisites.InstalledPackage) bool {
	if values == nil || len(values) > prerequisites.MaxAddedPackages {
		return false
	}
	for index, item := range values {
		if !prerequisites.ValidInstalledPackage(item) || index > 0 && values[index-1].NEVRA >= item.NEVRA {
			return false
		}
	}
	return true
}

func validHeldSet(transaction prerequisites.PendingTransaction) bool {
	if transaction.Start <= 0 || transaction.Held == nil || len(transaction.Held) > prerequisites.MaxHeldPackages {
		return false
	}
	for index, identity := range transaction.Held {
		if !prerequisites.ValidPackageIdentity(identity) || index > 0 && transaction.Held[index-1] >= identity {
			return false
		}
	}
	return true
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
	if !validSetupIdentity(pending.ID) || !validControllerDigest(pending.PlanDigest) {
		return state("pending setup identity is invalid")
	}
	if pending.Actions == nil || len(pending.Actions) > maxSetupActions || !tokens(pending.Actions) {
		return state("pending setup actions are invalid")
	}
	if pending.Requested == nil || len(pending.Requested) > maxSetupPackages || !slices.IsSorted(pending.Requested) {
		return state("pending package requirements exceed their bound")
	}
	for _, requirement := range pending.Requested {
		if !prerequisites.ValidHostRequirement(requirement) {
			return state("pending package requirement is invalid")
		}
	}
	if !validAddedSet(pending.Added) {
		return state("pending setup added packages are invalid")
	}
	if transaction := pending.Transaction; transaction != nil && !validHeldSet(*transaction) {
		return state("pending setup package transaction is invalid")
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
	if !validAddedSet(receipt.Added) {
		return state("setup receipt added packages are invalid")
	}
	for _, item := range receipt.Packages {
		run := validSetupIdentity(item.InstalledBy)
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

func validSetupIdentity(id string) bool {
	if _, ok := setupRunNumber(id); ok {
		return true
	}
	token, ok := strings.CutPrefix(id, installRunPrefix)
	return ok && len(token) == 32 && strings.Trim(token, "0123456789abcdef") == ""
}
