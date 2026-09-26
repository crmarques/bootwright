package installation

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
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

// The installed guest permits the reads completion is proved through. RHEL
// ships an allow list without them, so an installation that left the filter
// alone would answer a ping and refuse every file the marker is read from.
func TestThePostSectionPermitsTheIdentityReads(t *testing.T) {
	kickstart := kickstartOf(t, labCatalog())
	permitted := "guest-file-open,guest-file-close,guest-file-read"
	requireLine(t, kickstart, `if grep -q '^FILTER_RPC_ARGS=.*--allow-rpcs=' /etc/sysconfig/qemu-ga; then`)
	requireLine(t, kickstart, `sed -i '/^FILTER_RPC_ARGS=/s/--allow-rpcs=/--allow-rpcs=`+permitted+`,/' /etc/sysconfig/qemu-ga`)
	requireLine(t, kickstart, `grep -q '^FILTER_RPC_ARGS=.*--allow-rpcs=`+permitted+`,' /etc/sysconfig/qemu-ga`)
	requireLine(t, kickstart, `fi`)
}

// The installation republishes the host key it generated, because the guest
// agent is confined and cannot read sshd's own key directory. Generating the
// key here rather than at first boot means the published copy is the one sshd
// will present, so the two can never diverge unnoticed.
func TestThePostSectionRepublishesTheHostKeyItGenerated(t *testing.T) {
	kickstart := kickstartOf(t, labCatalog())
	requireLine(t, kickstart, "/usr/bin/ssh-keygen -A")
	requireLine(t, kickstart, "install -m 0444 /etc/ssh/ssh_host_ed25519_key.pub /etc/bootwright/host-key.pub")
	requireLine(t, kickstart, "test -s /etc/bootwright/host-key.pub")
	generated := strings.Index(kickstart, "ssh-keygen -A")
	published := strings.Index(kickstart, "install -m 0444 /etc/ssh")
	proved := strings.Index(kickstart, "test -s /etc/bootwright/host-key.pub")
	if generated < 0 || published < generated || proved < published {
		t.Fatalf("the host key is published before it is generated or proved:\n%s", kickstart)
	}
}

// The frozen request carries both paths the identity operation reads, so the
// adapter never names one of its own.
func TestTheRequestCarriesEveryPathTheIdentityOperationReads(t *testing.T) {
	request, _ := onlyRequest(t, labCatalog())
	if request.MarkerPath != MarkerPath || request.HostKeyPath != HostKeyPath {
		t.Fatalf("the request omits an identity path: %q %q", request.MarkerPath, request.HostKeyPath)
	}
	if request.Version != requestVersion {
		t.Fatalf("the request version is not the frozen one: %q", request.Version)
	}
}

// A guest agent whose filter this installation cannot recognize fails the
// installation, because the alternative is a guest that installs and then
// proves nothing for the identity operation's full retry budget.
func TestTheAgentFilterEditIsProvedRatherThanAssumed(t *testing.T) {
	kickstart := kickstartOf(t, labCatalog())
	edit := strings.Index(kickstart, "s/--allow-rpcs=")
	proof := strings.Index(kickstart, `grep -q '^FILTER_RPC_ARGS=.*--allow-rpcs=guest-file-open`)
	if edit < 0 || proof < 0 || proof < edit {
		t.Fatalf("the filter edit is not proved after it is made:\n%s", kickstart)
	}
	if !strings.Contains(kickstart, "%post --erroronfail") {
		t.Fatalf("an unproved filter would not fail the installation:\n%s", kickstart)
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

func physicalInstallation() Installation {
	return Installation{
		Address: "198.51.100.11", Channel: substrate.ChannelDeliveredKey,
		ExpectedMACs: []string{"52:54:00:9a:1b:01", "52:54:00:9a:1b:02"},
		Hostname:     "metal-01.metal.example.test", Interface: "eno1",
		InterfaceMAC: "52:54:00:9a:1b:01", Keyboard: "us", Language: "en_US.UTF-8",
		MarkerPath: MarkerPath, HostKeyPath: HostKeyPath, PackageSource: "cdrom",
		Physical: true, Prefix: 24, RootDevice: "/dev/sda", Timezone: "UTC", User: "bootwright",
	}
}

// A physical installation proves it is running on the declared machine before
// it touches storage, and the proof is a %pre that fails closed.
func TestAPhysicalInstallationProvesItsTargetBeforeClearingAnyDisk(t *testing.T) {
	rendered, err := RenderKickstart(physicalInstallation())
	if err != nil {
		t.Fatal(err)
	}
	proof := strings.Index(rendered, "%pre --erroronfail")
	if proof < 0 {
		t.Fatal("a physical installation renders no target proof")
	}
	for _, clause := range []string{"clearpart", "autopart", "ignoredisk"} {
		if at := strings.Index(rendered, clause); at >= 0 && at < proof {
			t.Fatalf("%s is rendered before the target proof", clause)
		}
	}
	for _, address := range physicalInstallation().ExpectedMACs {
		if !strings.Contains(rendered, "grep -Fqx '"+address+"'") {
			t.Fatalf("the proof does not require %s", address)
		}
	}
	if !strings.Contains(rendered, "lsblk -ndo TYPE") {
		t.Fatal("the proof does not require the root device to be a whole disk")
	}
}

// A physical installation clears exactly the disk its Machine names and no
// other. With no name it refuses rather than rendering, because the installer
// left to choose on operator-owned hardware clears every disk there is.
func TestAPhysicalKickstartNeverClearsADiskItDidNotName(t *testing.T) {
	rendered, err := RenderKickstart(physicalInstallation())
	if err != nil {
		t.Fatal(err)
	}
	requireLine(t, rendered, "ignoredisk --only-use=sda")
	for _, line := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(line, "clearpart") && line != "clearpart --all --initlabel --drives=sda" {
			t.Fatalf("a physical installation renders %q", line)
		}
	}
	unnamed := physicalInstallation()
	unnamed.RootDevice = ""
	rendered, err = RenderKickstart(unnamed)
	if rendered != "" {
		t.Fatalf("a physical installation naming no disk rendered:\n%s", rendered)
	}
	expectRefusal(t, err, "lifecycle.state")
}

// The whole physical Kickstart is fixed, because the installer reads all of it
// and the proof, the storage directives and the delivered key are only safe in
// exactly this order.
func TestAPhysicalKickstartIsRenderedExactly(t *testing.T) {
	rendered, err := RenderKickstart(physicalInstallation())
	if err != nil {
		t.Fatal(err)
	}
	const want = `# Generated by Bootwright. Do not edit.
text
eula --agreed
poweroff

cdrom
network --bootproto=static --device=52:54:00:9a:1b:01 --ip=198.51.100.11 --netmask=255.255.255.0 --hostname=metal-01.metal.example.test --onboot=on --activate

lang en_US.UTF-8
keyboard --vckeymap=us
timezone UTC --utc

rootpw --lock
user --name=bootwright --groups=wheel --lock
sshkey --username=bootwright "@@BOOTWRIGHT_AUTHORIZED_KEY@@"

%pre --erroronfail --interpreter=/bin/bash
set -euo pipefail
bootwright_observed=$(cat /sys/class/net/*/address | tr 'A-Z' 'a-z' | sort -u)
if ! printf '%s\n' "${bootwright_observed}" | grep -Fqx '52:54:00:9a:1b:01'; then
echo 'Bootwright: this machine does not report 52:54:00:9a:1b:01, which its declaration requires; refusing before any disk is touched.' >&2
exit 1
fi
if ! printf '%s\n' "${bootwright_observed}" | grep -Fqx '52:54:00:9a:1b:02'; then
echo 'Bootwright: this machine does not report 52:54:00:9a:1b:02, which its declaration requires; refusing before any disk is touched.' >&2
exit 1
fi
bootwright_root=$(readlink -f -- '/dev/sda' || true)
if [ -z "${bootwright_root}" ] || [ ! -b "${bootwright_root}" ]; then
echo 'Bootwright: the declared root device /dev/sda is not a block device here; refusing before any disk is touched.' >&2
exit 1
fi
if [ "$(lsblk -ndo TYPE -- "${bootwright_root}")" != disk ]; then
echo 'Bootwright: the declared root device /dev/sda is not a whole disk; refusing before any disk is touched.' >&2
exit 1
fi
%end

ignoredisk --only-use=sda
clearpart --all --initlabel --drives=sda
autopart --type=lvm

%packages
@^minimal-environment
%end

%post --erroronfail
set -eu
install -d -m 0755 /etc/bootwright
cat > /etc/bootwright/install-marker.json <<'BOOTWRIGHT_MARKER_EOF'
@@BOOTWRIGHT_MARKER@@
BOOTWRIGHT_MARKER_EOF
chmod 0444 /etc/bootwright/install-marker.json
install -d -m 0755 /etc/ssh
cat > /tmp/bootwright-artifact-ca.pem <<'BOOTWRIGHT_ARTIFACT_CA_EOF'
@@BOOTWRIGHT_ARTIFACT_CERTIFICATE@@
BOOTWRIGHT_ARTIFACT_CA_EOF
curl --fail --silent --show-error --cacert /tmp/bootwright-artifact-ca.pem --output /etc/ssh/ssh_host_ed25519_key '@@BOOTWRIGHT_PRIVATE_URL@@/identity'
curl --fail --silent --show-error --cacert /tmp/bootwright-artifact-ca.pem --output /etc/ssh/ssh_host_ed25519_key.pub '@@BOOTWRIGHT_PRIVATE_URL@@/identity.pub'
chmod 0600 /etc/ssh/ssh_host_ed25519_key
chmod 0644 /etc/ssh/ssh_host_ed25519_key.pub
test -s /etc/ssh/ssh_host_ed25519_key
/usr/bin/ssh-keygen -A
/usr/bin/ssh-keygen -y -f /etc/ssh/ssh_host_ed25519_key > /dev/null
shred -u /tmp/bootwright-artifact-ca.pem 2>/dev/null || rm -f /tmp/bootwright-artifact-ca.pem
install -d -m 0750 /etc/sudoers.d
printf '%s\n' 'bootwright ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/60-bootwright
chmod 0440 /etc/sudoers.d/60-bootwright
install -d -m 0755 /etc/ssh/sshd_config.d
printf '%s\n' 'PasswordAuthentication no' 'PermitRootLogin prohibit-password' > /etc/ssh/sshd_config.d/60-bootwright.conf
chmod 0600 /etc/ssh/sshd_config.d/60-bootwright.conf
rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /run/install/ks.cfg
%end
`
	if rendered != want {
		t.Fatalf("physical kickstart =\n%s\nwant\n%s", rendered, want)
	}
}

// The machine is addressed by its hardware address, because the interface name
// a booted installer assigns is not the name the declaration used.
func TestAPhysicalInstallationAddressesItsInterfaceByHardwareAddress(t *testing.T) {
	rendered, err := RenderKickstart(physicalInstallation())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "--device=52:54:00:9a:1b:01") {
		t.Fatal("the network line does not address the interface by its hardware address")
	}
}

// A delivered key is installed before any key is generated, fetched over a
// verified connection, and the material is removed from the installer once it
// is placed. Nothing about the guest agent is rendered for a machine that has
// none.
func TestADeliveredKeyIsInstalledBeforeAnyKeyIsGenerated(t *testing.T) {
	rendered, err := RenderKickstart(physicalInstallation())
	if err != nil {
		t.Fatal(err)
	}
	fetch := strings.Index(rendered, PrivateURLToken)
	generate := strings.Index(rendered, "ssh-keygen -A")
	if fetch < 0 || generate < 0 || fetch > generate {
		t.Fatal("the delivered key is not installed before the host keys are generated")
	}
	if !strings.Contains(rendered, "--cacert") || strings.Contains(rendered, "--insecure") {
		t.Fatal("the private fetch does not verify the server it fetches from")
	}
	// Nothing about a guest agent is rendered, and the key is not republished
	// where an agent would have read it: the machine answers with it directly.
	for _, absent := range []string{agentFilter, "qemu-guest-agent", HostKeyPath} {
		if strings.Contains(rendered, absent) {
			t.Fatalf("a machine with no guest agent renders %q", absent)
		}
	}
}

// A machine its substrate created renders none of the physical arms, so one
// contract serves both without either leaking into the other.
func TestAVirtualInstallationRendersNoPhysicalProof(t *testing.T) {
	input := physicalInstallation()
	input.Channel, input.Physical, input.ExpectedMACs, input.InterfaceMAC = substrate.ChannelGuestAgent, false, nil, ""
	rendered, err := RenderKickstart(input)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered, "%pre") || strings.Contains(rendered, PrivateURLToken) {
		t.Fatal("a virtual installation renders a physical proof or a private fetch")
	}
	if !strings.Contains(rendered, "--device=eno1") {
		t.Fatal("a virtual installation does not address its interface by name")
	}
}
