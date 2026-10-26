package agentinstall

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// DiskEncryption uses only the agent installer's admitted role selectors.
type DiskEncryption struct {
	EnableOn string `json:"enableOn"`
	Mode     string `json:"mode"`
}

func diskEncryption(cluster api.Object) *DiskEncryption {
	disk := cluster.Spec().Get("security", "diskEncryption")
	if !disk.Present() {
		return nil
	}
	roles := disk.Get("roles").Strings()
	all := true
	for _, node := range cluster.Spec().Get("nodes").Items() {
		if !encryptedRole(cluster, node.Get("role").Text()) {
			all = false
		}
	}
	enableOn := "workers"
	if all {
		enableOn = "all"
	} else if slices.Contains(roles, "master") {
		enableOn = "masters"
	}
	return &DiskEncryption{EnableOn: enableOn, Mode: "tpmv2"}
}

func encryptedRole(cluster api.Object, role string) bool {
	roles := cluster.Spec().Get("security", "diskEncryption", "roles").Strings()
	if role == "master" {
		return slices.Contains(roles, role)
	}
	return (role == "worker" || role == "infra") && (slices.Contains(roles, "worker") || slices.Contains(roles, "infra"))
}

func encryptionRefusal(catalog api.Catalog, cluster api.Object) (string, string) {
	if !cluster.Spec().Has("security", "diskEncryption") {
		return "", ""
	}
	roles := cluster.Spec().Get("security", "diskEncryption", "roles").Strings()
	if len(roles) == 0 {
		return "disk encryption requires normalized role selection", "declare security.diskEncryption.roles on " + cluster.Identity()
	}
	for _, node := range cluster.Spec().Get("nodes").Items() {
		role := node.Get("role").Text()
		if !encryptedRole(cluster, role) {
			continue
		}
		machine, found := catalog.Find(api.Machine, node.Get("machineRef").Text())
		if !found {
			continue
		}
		provider, found := catalog.Find(api.InfraProvider, machine.Spec().Get("substrate", "providerRef").Text())
		if !found || !provider.Spec().Has("libvirt") {
			continue
		}
		for _, profile := range provider.Spec().Get("libvirt", "machineProfiles").Items() {
			if profile.Get("name").Text() == machine.Spec().Get("substrate", "profileRef").Text() && !profile.Has("tpm") {
				return "an encrypted node's libvirt profile declares no TPM", "add tpm: {} to the selected profile on " + provider.Identity()
			}
		}
	}
	return "", ""
}
