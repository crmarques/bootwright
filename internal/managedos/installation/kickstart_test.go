package installation

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func kickstartOf(t *testing.T, catalog api.Catalog) string {
	t.Helper()
	request, _ := onlyRequest(t, catalog)
	return request.Kickstart
}

func requireLine(t *testing.T, kickstart, line string) {
	t.Helper()
	for _, candidate := range strings.Split(kickstart, "\n") {
		if candidate == line {
			return
		}
	}
	t.Fatalf("the kickstart has no line %q:\n%s", line, kickstart)
}

// The derived installation is unattended and complete: every directive comes
// from effective state, and nothing prompts.
func TestTheDerivedKickstartInstallsUnattended(t *testing.T) {
	kickstart := kickstartOf(t, labCatalog())
	for _, line := range []string{
		"text",
		"eula --agreed",
		"poweroff",
		"url --url=http://192.0.2.1:8080/os/rhel-9-8/tree",
		"lang en_US.UTF-8",
		"keyboard --vckeymap=us",
		"timezone Etc/UTC --utc --ntpservers=192.0.2.1",
		"rootpw --lock",
		"user --name=bootwright --groups=wheel --lock",
		"ignoredisk --only-use=vda",
		"clearpart --all --initlabel --drives=vda",
		"autopart --type=lvm",
		"selinux --enforcing",
		"services --enabled=chronyd,sshd",
	} {
		requireLine(t, kickstart, line)
	}
}

// The one static assignment comes from the Machine's own selected address, its
// prefix, the template's default route and the selected resolvers.
func TestTheNetworkLineComesFromTheSelectedAddress(t *testing.T) {
	kickstart := kickstartOf(t, labCatalog())
	requireLine(t, kickstart, "network --bootproto=static --device=enp1s0 --ip=198.51.100.11 --netmask=255.255.255.0 --hostname=rhel-01.lab.example.test --onboot=on --activate --gateway=198.51.100.1 --nameserver=192.0.2.1")
}

// A template with no default route installs without one rather than inventing
// a gateway the graph never declared.
func TestATemplateWithoutADefaultRouteRendersNoGateway(t *testing.T) {
	bare := api.NewObject(api.NetworkConfig, "lab-guests", api.Value{}, api.MapValue(
		field("machineNetwork", api.ListValue(api.MapValue(text("cidr", "198.51.100.0/24")))),
		field("nmstate", api.MapValue(field("interfaces", api.ListValue(
			api.MapValue(text("name", "enp1s0"), text("type", "ethernet")),
		)))),
	))
	kickstart := kickstartOf(t, labCatalog(bare))
	if strings.Contains(kickstart, "--gateway=") || strings.Contains(kickstart, "--nameserver=") {
		t.Fatalf("a bare template invented routing:\n%s", kickstart)
	}
}

// The guest agent is on every libvirt Machine, because the proof this contract
// reads completion through goes over its channel.
func TestTheGuestAgentIsAlwaysInstalled(t *testing.T) {
	kickstart := kickstartOf(t, labCatalog())
	requireLine(t, kickstart, "qemu-guest-agent")
	requireLine(t, kickstart, "chrony")
	requireLine(t, kickstart, "@^minimal-environment")
	requireLine(t, kickstart, "%packages --excludedocs --exclude-weakdeps")
}

// The account is authorized by a public key the adapter substitutes, and the
// marker likewise, so the frozen request carries neither.
func TestTheKickstartCarriesTokensRatherThanValues(t *testing.T) {
	kickstart := kickstartOf(t, labCatalog())
	if !strings.Contains(kickstart, AuthorizedKeyToken) || !strings.Contains(kickstart, MarkerToken) {
		t.Fatalf("the kickstart resolved a value at plan time:\n%s", kickstart)
	}
	requireLine(t, kickstart, `sshkey --username=bootwright "`+AuthorizedKeyToken+`"`)
	for _, forbidden := range []string{"PRIVATE KEY", "--plaintext", "--iscrypted", "sshpw"} {
		if strings.Contains(kickstart, forbidden) {
			t.Fatalf("the kickstart carries %q", forbidden)
		}
	}
}

// The %post leaves the proof, the escalation and the daemon policy, and removes
// every retained copy of itself.
func TestThePostSectionLeavesTheProofAndNoCopyOfItself(t *testing.T) {
	kickstart := kickstartOf(t, labCatalog())
	requireLine(t, kickstart, "%post --erroronfail")
	requireLine(t, kickstart, "cat > /etc/bootwright/install-marker.json <<'BOOTWRIGHT_MARKER_EOF'")
	requireLine(t, kickstart, "chmod 0440 /etc/sudoers.d/60-bootwright")
	requireLine(t, kickstart, "rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /run/install/ks.cfg")
	if strings.Count(kickstart, "%end") != 2 {
		t.Fatalf("sections are unbalanced:\n%s", kickstart)
	}
}

// A Machine with no root-device hint lets the installer choose, because a
// predicate the graph never declared is not a target selector.
func TestAMachineWithoutHintsPartitionsAutomatically(t *testing.T) {
	noHints := guest(field("os", api.MapValue(
		field("provided", api.BoolValue(false)), text("installProfileRef", "rhel-9-8"),
	)))
	kickstart := kickstartOf(t, labCatalog(noHints))
	if strings.Contains(kickstart, "ignoredisk") {
		t.Fatalf("an undeclared device was selected:\n%s", kickstart)
	}
	requireLine(t, kickstart, "clearpart --all --initlabel")
}

// One revision always renders one file, because every derived list is ordered
// and every default is explicit.
func TestRenderingIsDeterministic(t *testing.T) {
	first := kickstartOf(t, labCatalog())
	second := kickstartOf(t, labCatalog())
	if first != second {
		t.Fatal("two renderings of one revision differ")
	}
	if strings.Contains(first, "\n\n\n") || !strings.HasSuffix(first, "\n") {
		t.Fatalf("the rendering is not normalized:\n%q", first)
	}
}

func TestRenderingRefusesAnAddressItCannotUse(t *testing.T) {
	for name, input := range map[string]Installation{
		"not an address": {Address: "not-an-address", Prefix: 24, Interface: "enp1s0"},
		"no prefix":      {Address: "198.51.100.11", Interface: "enp1s0"},
		"prefix too big": {Address: "198.51.100.11", Prefix: 33, Interface: "enp1s0"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := RenderKickstart(input); err == nil {
				t.Fatal("an unusable address was accepted")
			}
		})
	}
}

func TestNetmasksRenderFromTheirPrefix(t *testing.T) {
	for prefix, want := range map[int]string{8: "255.0.0.0", 16: "255.255.0.0", 24: "255.255.255.0", 25: "255.255.255.128", 32: "255.255.255.255"} {
		if got := netmask(prefix); got != want {
			t.Fatalf("/%d = %q, want %q", prefix, got, want)
		}
	}
}
