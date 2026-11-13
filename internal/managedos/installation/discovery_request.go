package installation

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const DiscoveryImplementation = "os-disk-discovery-v1"
const DiscoveryFamily = "os-disk-discovery"
const DiscoveryPreparation = "physical-disk-inventory"
const discoveryVersion = "os-disk-discovery-v1"
const discoveryVariable = "bootwright_disk_discovery"

type DiskProtections func(api.Catalog, api.Object) ([]diskselection.DiskIdentity, error)

type DiscoveryNetwork struct {
	Address     string   `json:"address"`
	BondOptions []string `json:"bondOptions,omitempty"`
	BondPorts   []string `json:"bondPorts,omitempty"`
	Gateway     string   `json:"gateway,omitempty"`
	Interface   string   `json:"interface"`
	Prefix      int      `json:"prefix"`
	Type        string   `json:"type"`
	VLANBase    string   `json:"vlanBase,omitempty"`
	VLANID      int      `json:"vlanID,omitempty"`
}

type DiscoveryRequest struct {
	BootMedia       Media                `json:"bootMedia"`
	Controller      Controller           `json:"controller"`
	DeadlineSeconds int                  `json:"deadlineSeconds"`
	Identity        Identity             `json:"identity"`
	Interfaces      []Interface          `json:"interfaces"`
	Network         DiscoveryNetwork     `json:"network"`
	Placement       machineref.Placement `json:"placement"`
	Private         Publication          `json:"private"`
	Version         string               `json:"version"`
}

func DiscoveryBlockID(name string) string {
	return reconciliation.BlockIdentity("os-disk-discovery-", name)
}
func DiscoveryContentDigest() string {
	h := sha256.Sum256([]byte(DiscoveryImplementation + ":" + discoveryVersion + ":private-custom-init-readonly-inventory-pinned-tls-receipt-cleanup-v3"))
	return hex.EncodeToString(h[:])
}
func (r DiscoveryRequest) Canonical() ([]byte, error) {
	return reconciliation.Freeze(r, "disk discovery")
}
func DecodeDiscoveryRequest(data []byte) (DiscoveryRequest, error) {
	return reconciliation.ThawVersion[DiscoveryRequest](data, "disk discovery", discoveryVersion)
}
func (r DiscoveryRequest) SecretReferences() []string {
	refs := []string{r.Controller.CredentialsRef, r.Controller.TrustBundleRef, r.Private.CertificateRef}
	refs = slices.DeleteFunc(refs, func(s string) bool { return s == "" })
	slices.Sort(refs)
	return slices.Compact(refs)
}

func discoveryRequest(catalog api.Catalog, m api.Object, input lifecycle.PlanInput, records map[string]MediaRecord) (DiscoveryRequest, Requirements, error) {
	boot, needs, err := deriveRequest(catalog, m, input.Controller, input.Context.Name, nil, false)
	if err != nil {
		return DiscoveryRequest{}, needs, err
	}
	if !boot.Target.Physical || !boot.Placement.Local() || boot.Private == nil || boot.TLSCertificateRef == "" || boot.Target.Hardware == nil || boot.Target.Controller.TLSVerification == "none" || !boot.Target.Controller.TLSVerify {
		return DiscoveryRequest{}, needs, refusal("lifecycle.unsupported", "disk discovery requires physical RHEL, controller-local private HTTPS media and verified BMC TLS", "use a controller-local HTTPS artifact server and full or certificate BMC verification")
	}
	boot, err = pinMedia(catalog, boot, records)
	if err != nil {
		return DiscoveryRequest{}, needs, err
	}
	if err := refusedReleases(catalog, boot, records); err != nil {
		return DiscoveryRequest{}, needs, err
	}
	profile, _ := catalog.Find(api.MachineInstallProfile, boot.Identity.Profile)
	install, err := installationFor(catalog, m, profile, boot, &needs)
	if err != nil {
		return DiscoveryRequest{}, needs, err
	}
	r := DiscoveryRequest{Version: discoveryVersion, Placement: boot.Placement, Identity: boot.Identity, BootMedia: boot.BootMedia, Controller: boot.Target.Controller, Interfaces: boot.Target.Hardware.Interfaces, Private: *boot.Private, DeadlineSeconds: 1800,
		Network: DiscoveryNetwork{Address: install.Address, Prefix: install.Prefix, Gateway: install.Gateway, Interface: install.Interface, Type: install.InterfaceType, VLANBase: install.VLANBase, VLANID: install.VLANID, BondPorts: install.BondPorts, BondOptions: install.BondOptions}}
	r.Identity.Block = DiscoveryBlockID(m.Name())
	r.Private.CertificateRef = boot.TLSCertificateRef
	if _, err := networkLine(install); err != nil {
		return DiscoveryRequest{}, needs, err
	}
	r.Private.Path += "/discovery"
	r.Private.URL += "/discovery"
	return r, needs, nil
}
