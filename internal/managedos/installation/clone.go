package installation

import (
	"fmt"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate"
)

// CloudInit carries the public parts of the seed. Only the execution receives
// the realized instance identity, install marker, fleet key and host key pair.
type CloudInit struct {
	GrowRootFilesystem bool           `json:"growRootFilesystem"`
	KeyType            string         `json:"keyType"`
	Network            map[string]any `json:"network"`
	Release            string         `json:"release"`
	RootDiskBytes      int64          `json:"rootDiskBytes"`
	UserData           map[string]any `json:"userData"`
}

func clonePermitted(catalog api.Catalog, m, profile api.Object) bool {
	provider, hardware, ok := substrate.SelectedProfile(catalog, m)
	arm := substrate.Variant(provider)
	return ok && substrate.SupportsTemplateClone(arm) && hardware.Has(substrate.TemplateSourceField(arm)) && profile.Spec().Has("installer", "templateClone", "seed", "cloudInit")
}

func cloneRequest(catalog api.Catalog, m, profile api.Object, controller, contextName string) (Request, Requirements, error) {
	if !clonePermitted(catalog, m, profile) {
		return Request{}, Requirements{}, installUnsupported.Fail("the clone requires a Machine profile with a source template and cloud-init seed support", "select a machine profile that supports template cloning on "+m.Identity())
	}
	target, err := targetFor(catalog, m, contextName, controller)
	if err != nil {
		return Request{}, Requirements{}, err
	}
	if target.HostKeyType, err = installationHostKey(catalog, m, profile, target); err != nil {
		return Request{}, Requirements{}, err
	}
	host, _ := catalog.Find(api.Machine, controller)
	placement, err := machine.PlacementFor(host, controller)
	if err != nil {
		return Request{}, Requirements{}, err
	}
	request := Request{Identity: Identity{Block: BlockID(m.Name()), Context: contextName, Object: m.Name(), Profile: profile.Name()},
		Target: target, Placement: placement, FleetKeyRef: fleetKeyRef(catalog), HostKeyPath: HostKeyPath, MarkerPath: MarkerPath, User: installUser,
		Budgets: installationBudgets, Version: requestVersion, InstallerArguments: []string{}}
	if request.FleetKeyRef == "" {
		return Request{}, Requirements{}, refusal("api.required", "the Environment declares no fleet access key", "set spec.remoteMachinesAccessKey.keyRef")
	}
	if request.Registration, err = registrationFor(catalog, profile); err != nil {
		return Request{}, Requirements{}, err
	}
	if request.Update, err = updateFor(profile); err != nil {
		return Request{}, Requirements{}, err
	}
	needs := Requirements{Machine: m.Name()}
	install, err := InstallationFor(catalog, m, profile, request, &needs)
	if err != nil {
		return Request{}, Requirements{}, err
	}
	if err := guardKickstartValues(install); err != nil {
		return Request{}, Requirements{}, err
	}
	request.Address, request.Hostname = install.Address, install.Hostname
	if request.Network, err = networkFor(catalog, m, install); err != nil {
		return Request{}, Requirements{}, err
	}
	pins, err := installerArguments(catalog, m, request)
	if err != nil {
		return Request{}, Requirements{}, err
	}
	grow := profile.Spec().Get("installer", "templateClone", "seed", "cloudInit", "growRootFilesystem")
	key := deliveredHostKeys[target.HostKeyType]
	cloud := CloudInit{GrowRootFilesystem: !grow.Present() || grow.Bool(), Release: profile.Spec().Get("os", "version").Text(),
		Network: cloneNetwork(install, pins), KeyType: strings.TrimSuffix(strings.TrimPrefix(key.path, "/etc/ssh/ssh_host_"), "_key")}
	_, hardware, _ := substrate.SelectedProfile(catalog, m)
	sizeGiB, _ := hardware.Get("diskGiB").Int64()
	cloud.RootDiskBytes = sizeGiB << 30
	if cloud.GrowRootFilesystem && cloud.RootDiskBytes <= 0 {
		return Request{}, Requirements{}, installUnsupported.Fail("the growing root filesystem requires a positive template profile disk size", "set diskGiB on the machine profile")
	}
	cloud.UserData = cloneUserData(install, cloud, key.path)
	request.CloudInit = &cloud
	request.freezeDerived()
	needs.DNSServers, needs.NTPServers = SortedUnique(needs.DNSServers), SortedUnique(needs.NTPServers)
	return request, needs, nil
}

// The VMware datasource accepts the ordinary cloud-init network v1 format.
// Only bootstrap links are configured here. The full frozen NMState document
// is applied and read back over the pinned fleet connection after first boot.
func cloneNetwork(i Installation, pins []string) map[string]any {
	var links []map[string]any
	for _, pin := range pins {
		name, mac, _ := strings.Cut(strings.TrimPrefix(pin, "ifname="), ":")
		links = append(links, map[string]any{"type": "physical", "name": name, "mac_address": mac})
	}
	if len(i.BondPorts) > 0 {
		name := i.Interface
		if i.VLANBase != "" {
			name = i.VLANBase
		}
		options := map[string]string{}
		for _, opt := range i.BondOptions {
			k, v, _ := strings.Cut(opt, "=")
			options["bond-"+strings.ReplaceAll(k, "_", "-")] = v
		}
		links = append(links, map[string]any{"type": "bond", "name": name, "bond_interfaces": i.BondPorts, "params": options})
	}
	if i.VLANBase != "" {
		links = append(links, map[string]any{"type": "vlan", "name": i.Interface, "vlan_id": i.VLANID, "vlan_link": i.VLANBase})
	}
	address := map[string]any{"type": "static", "address": fmt.Sprintf("%s/%d", i.Address, i.Prefix)}
	if i.Gateway != "" {
		address["gateway"] = i.Gateway
	}
	for _, link := range links {
		if link["name"] == i.Interface {
			link["subnets"] = []any{address}
		}
	}
	if len(i.Nameservers) > 0 {
		links = append(links, map[string]any{"type": "nameserver", "address": i.Nameservers})
	}
	return map[string]any{"version": 1, "config": links}
}

func cloneUserData(i Installation, cloud CloudInit, keyPath string) map[string]any {
	files := []map[string]string{{"path": "/etc/ssh/sshd_config.d/00-bootwright-host-key.conf", "permissions": "0644", "content": "HostKey " + keyPath + "\n"}}
	for _, repository := range i.Repositories {
		files = append(files, map[string]string{"path": repositoryPath(repository), "owner": "root:root", "permissions": "0644",
			"content": strings.Join(repositoryContents(repository), "\n") + "\n"})
	}
	if i.Release != "" {
		files = append(files, map[string]string{"path": ReleaseVariablePath, "owner": "root:root", "permissions": "0644", "content": i.Release + "\n"})
	}
	commands := []any{"set -eu"}
	for _, s := range i.DisabledServices {
		commands = append(commands, []string{"/usr/bin/systemctl", "disable", "--now", "--", s})
	}
	for _, s := range i.EnabledServices {
		commands = append(commands, []string{"/usr/bin/systemctl", "enable", "--now", "--", s})
	}
	commands = append(commands, []string{"/usr/bin/systemctl", "reload", "sshd"})
	grow := "off"
	if cloud.GrowRootFilesystem {
		grow = "auto"
	}
	config := map[string]any{"preserve_hostname": false, "fqdn": i.Hostname, "disable_root": true, "ssh_pwauth": false, "ssh_deletekeys": true,
		"ssh_genkeytypes": []string{}, "no_ssh_fingerprints": true, "ssh": map[string]bool{"emit_keys_to_console": false},
		"growpart": map[string]any{"mode": grow, "devices": []string{"/"}}, "resize_rootfs": cloud.GrowRootFilesystem,
		"runcmd": commands, "write_files": files}
	if len(i.NTPServers) > 0 {
		config["ntp"] = map[string]any{"enabled": true, "ntp_client": "chrony", "servers": i.NTPServers}
	}
	return config
}
