package diskdiscovery

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

const DiscoveryImplementation = "os-disk-discovery-v1"
const DiscoveryPreparation = "physical-disk-inventory"
const discoveryVersion = "os-disk-discovery-v1"
const discoveryVariable = "bootwright_disk_discovery"

const InventoryAutomatic = "automatic"
const InventoryBoot = "boot"

type DiskProtections func(api.Catalog, api.Object) ([]diskselection.DiskIdentity, error)

type DiscoveryNetwork struct {
	Address     string   `json:"address"`
	BondOptions []string `json:"bondOptions,omitempty"`
	BondPorts   []string `json:"bondPorts,omitempty"`
	Gateway     string   `json:"gateway,omitempty"`
	Interface   string   `json:"interface"`
	Nameservers []string `json:"nameservers,omitempty"`
	Prefix      int      `json:"prefix"`
	Type        string   `json:"type"`
	VLANBase    string   `json:"vlanBase,omitempty"`
	VLANID      int      `json:"vlanID,omitempty"`
}

type DiscoveryRequest struct {
	BootMedia       installation.Media       `json:"bootMedia"`
	Controller      installation.Controller  `json:"controller"`
	DeadlineSeconds int                      `json:"deadlineSeconds"`
	Identity        installation.Identity    `json:"identity"`
	Interfaces      []installation.Interface `json:"interfaces"`
	Inventory       string                   `json:"inventory"`
	MachinePort     substrate.MachinePort    `json:"machinePort"`
	Network         DiscoveryNetwork         `json:"network"`
	Placement       machineref.Placement     `json:"placement"`
	Private         installation.Publication `json:"private"`
	SSH             DiscoverySSH             `json:"ssh"`
	Version         string                   `json:"version"`
}

type DiscoverySSH struct {
	Port int `json:"port"`
}

func DiscoveryBlockID(name string) string {
	return reconciliation.BlockIdentity("os-disk-discovery-", name)
}
func DiscoveryContentDigest() string {
	h := sha256.Sum256([]byte(DiscoveryImplementation + ":" + discoveryVersion + ":bmc-drive-inventory-or-minimal-ram-boot-pinned-ssh-bmc-poweroff-v1" + ":" + substrate.RedfishMediaPolicy))
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

func discoveryRequest(catalog api.Catalog, m api.Object, input lifecycle.PlanInput, records map[string]installation.MediaRecord) (DiscoveryRequest, installation.Requirements, error) {
	boot, needs, err := installation.DeriveRequest(catalog, m, input.Controller, input.Context.Name, nil, false)
	if err != nil {
		return DiscoveryRequest{}, needs, err
	}
	if !boot.Target.Physical || !boot.Placement.Local() || boot.Private == nil || boot.TLSCertificateRef == "" || boot.Target.Hardware == nil || boot.Target.Controller.TLSVerification == "none" || !boot.Target.Controller.TLSVerify {
		return DiscoveryRequest{}, needs, discoveryUnsupported.Fail("disk discovery requires physical RHEL, controller-local private HTTPS media and verified BMC TLS", "use a controller-local HTTPS artifact server and full or certificate BMC verification")
	}
	boot, err = installation.PinMedia(catalog, boot, records)
	if err != nil {
		return DiscoveryRequest{}, needs, err
	}
	if err := installation.RefusedReleases(catalog, boot, records); err != nil {
		return DiscoveryRequest{}, needs, err
	}
	profile, _ := catalog.Find(api.MachineInstallProfile, boot.Identity.Profile)
	install, err := installation.InstallationFor(catalog, m, profile, boot, &needs)
	if err != nil {
		return DiscoveryRequest{}, needs, err
	}
	r := DiscoveryRequest{Version: discoveryVersion, Placement: boot.Placement, Identity: boot.Identity, BootMedia: boot.BootMedia, Controller: boot.Target.Controller, MachinePort: boot.Target.MachinePort, Interfaces: boot.Target.Hardware.Interfaces, Inventory: discoveryInventory(m), Private: *boot.Private, DeadlineSeconds: 1800,
		SSH:     DiscoverySSH{Port: discoverySSHPort(m)},
		Network: DiscoveryNetwork{Address: install.Address, Prefix: install.Prefix, Gateway: install.Gateway, Interface: install.Interface, Nameservers: install.Nameservers, Type: install.InterfaceType, VLANBase: install.VLANBase, VLANID: install.VLANID, BondPorts: install.BondPorts, BondOptions: install.BondOptions}}
	r.Identity.Block = DiscoveryBlockID(m.Name())
	r.Private.CertificateRef = boot.TLSCertificateRef
	if _, err := installation.NetworkLine(install); err != nil {
		return DiscoveryRequest{}, needs, err
	}
	r.Private.Path += "/discovery"
	r.Private.URL += "/discovery"
	if !strings.HasPrefix(r.Private.URL, "https://") {
		return DiscoveryRequest{}, needs, discoveryUnsupported.Fail("disk discovery publishes its private inventory media only over HTTPS", "select a controller-local HTTPS artifact server")
	}
	return r, needs, nil
}

func discoverySSHPort(m api.Object) int {
	if port, ok := substrate.MachineDiscoveryPolicy(m).Get("ssh", "port").Int64(); ok {
		return int(port)
	}
	return 22
}

func discoveryInventory(m api.Object) string {
	if substrate.MachineDiscoveryPolicy(m).Get("inventory").Text() == InventoryBoot {
		return InventoryBoot
	}
	return InventoryAutomatic
}
