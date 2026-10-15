package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/substrate"
)

func TestTheNetworkVariantsExampleInstallsEachInterfaceShape(t *testing.T) {
	const bondOptions = "--bondslaves=nic0,nic1 --bondopts=mode=active-backup,miimon=100"
	want := map[string]struct {
		networkLine, gateway, document string
		ports                          []string
	}{
		"nv-bond": {
			networkLine: "network --bootproto=static --device=bond0 " + bondOptions + " --ip=198.51.100.11 --netmask=255.255.255.192 ",
			gateway:     " --gateway=198.51.100.1 ",
			document:    `{"ip":"203.0.113.11","prefix-length":24}`,
			ports:       []string{"nic0", "nic1"},
		},
		"nv-vlan": {
			networkLine: "network --bootproto=static --device=nic0 --vlanid=120 --interfacename=nic0.120 --ip=198.51.100.71 --netmask=255.255.255.192 ",
			gateway:     " --gateway=198.51.100.65 ",
			document:    `{"ip":"203.0.113.12","prefix-length":24}`,
			ports:       []string{"nic0"},
		},
		"nv-vlan-bond": {
			networkLine: "network --bootproto=static --device=bond0 " + bondOptions + " --vlanid=130 --interfacename=bond0.130 --ip=198.51.100.141 --netmask=255.255.255.192 ",
			gateway:     " --gateway=198.51.100.129 ",
			document:    `{"ip":"203.0.113.13","prefix-length":24}`,
			ports:       []string{"nic0", "nic1"},
		},
	}
	requests := plannedInstallations(t, "libvirt-network-variants", "controller", "lab")
	if len(requests) != len(want) {
		t.Fatalf("planned %d installations, want %d", len(requests), len(want))
	}
	for _, request := range requests {
		machine := request.Identity.Object
		expected, ok := want[machine]
		if !ok {
			t.Fatalf("an installation of unexpected object %q", machine)
		}
		t.Run(machine, func(t *testing.T) {
			var lines []string
			for _, line := range strings.Split(request.Kickstart, "\n") {
				if strings.HasPrefix(line, "network ") {
					lines = append(lines, line)
				}
			}
			if len(lines) != 1 {
				t.Fatalf("network lines = %q, want exactly one", lines)
			}
			if !strings.HasPrefix(lines[0], expected.networkLine) {
				t.Fatalf("network line = %q, want prefix %q", lines[0], expected.networkLine)
			}
			if !strings.Contains(lines[0], expected.gateway) || strings.Contains(lines[0], "--mtu") {
				t.Fatalf("network line = %q, want %q and no --mtu", lines[0], expected.gateway)
			}
			var arguments []string
			for _, port := range expected.ports {
				arguments = append(arguments, "ifname="+port+":"+substrate.InterfaceMAC("lab", machine, port))
			}
			if strings.Join(request.InstallerArguments, "\n") != strings.Join(arguments, "\n") {
				t.Fatalf("installer arguments = %q, want %q", request.InstallerArguments, arguments)
			}
			document, err := json.Marshal(request.Network.Document)
			if err != nil {
				t.Fatalf("encoding the network document: %v", err)
			}
			for _, fragment := range []string{`"mtu":1400`, expected.document, `"destination":"192.0.2.0/24"`} {
				if !strings.Contains(string(document), fragment) {
					t.Fatalf("network document %s lacks %s", document, fragment)
				}
			}
		})
	}
}
