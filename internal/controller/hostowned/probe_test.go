package hostowned

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const namespace = "https://bootwright.io/substrate/v1"

type host struct {
	answers  map[string]result
	missing  []string
	foreign  []string
	socket   bool
	quadlets []string
	ran      []string
}

func (h *host) probe() *Probe {
	return &Probe{
		config: Config{Prefix: "bootwright-", Namespace: namespace, ImagePrefix: "/var/lib/libvirt/images/bootwright"},
		run: func(_ context.Context, path string, args []string) (result, error) {
			line := strings.Join(append([]string{path}, args...), " ")
			h.ran = append(h.ran, line)
			answer, found := h.answers[line]
			if !found {
				return result{}, nil
			}
			return answer, nil
		},
		qualify: func(path string) (string, error) {
			switch {
			case slices.Contains(h.missing, path):
				return "", fs.ErrNotExist
			case slices.Contains(h.foreign, path):
				return "", errors.New("unqualified")
			}
			return path, nil
		},
		sockets: func(string) (bool, error) { return h.socket, nil },
		entries: func(string) ([]string, error) {
			if h.quadlets == nil {
				return nil, fs.ErrNotExist
			}
			return h.quadlets, nil
		},
	}
}

func virsh(args string) string { return virshPath + " -c " + connection + " -q " + args }

func units(command string) string {
	if command == "list-units" {
		return systemctlPath + " list-units --all --plain --no-legend --no-pager --full -- bootwright-*"
	}
	return systemctlPath + " list-unit-files --no-legend --no-pager --full -- bootwright-*"
}

func out(text string) result { return result{stdout: []byte(text)} }

func refused(t *testing.T, err error, message string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != uninstallCode || !strings.Contains(reported[0].Message, message) {
		t.Fatalf("refusal = %#v, want %q", reported, message)
	}
}

// Every kind of owned object is found: a libvirt domain, network and pool by
// name, by the ownership namespace in its XML or, for a pool, by a target
// beneath the image prefix; a systemd unit loaded or only installed; a
// quadlet source; and a podman container. What carries no mark is ignored.
func TestEveryOwnedObjectKindIsFound(t *testing.T) {
	h := &host{answers: map[string]result{
		virsh("list --all --name"):                   out("bootwright-lab-rhel-01\nguest\noperator-vm\n\n"),
		virsh("dumpxml --inactive -- guest"):         out(`<domain><name>guest</name><metadata><bw:owner xmlns:bw="` + namespace + `"><bw:context>lab</bw:context></bw:owner></metadata></domain>`),
		virsh("dumpxml --inactive -- operator-vm"):   out(`<domain><name>operator-vm</name><metadata><x:tag xmlns:x="https://example.com/x"/></metadata></domain>`),
		virsh("net-list --all --name"):               out("bootwright-lab-provisioning\ndefault\n"),
		virsh("net-dumpxml --inactive -- default"):   out(`<network><name>default</name></network>`),
		virsh("pool-list --all --name"):              out("images\nmedia\n"),
		virsh("pool-dumpxml --inactive -- images"):   out(`<pool type="dir"><name>images</name><target><path>/var/lib/libvirt/images</path></target></pool>`),
		virsh("pool-dumpxml --inactive -- media"):    out(`<pool type="dir"><name>media</name><target><path>/var/lib/libvirt/images/bootwright/lab/providers/kvm/vmedia</path></target></pool>`),
		units("list-units"):                          out("bootwright-lab-bmc-rhel-01.service loaded active running Emulated BMC\n"),
		units("list-unit-files"):                     out("bootwright-lab-bmc-rhel-01.service generated -\nbootwright-lab-dns-server-dns.service disabled enabled\n"),
		podmanPath + " ps --all --format {{.Names}}": out("bootwright-lab-artifacts-web\nunrelated\n"),
	}, quadlets: []string{"bootwright-lab-dns-server-dns.container", "bootwright-notes.txt", "other.container"}}
	owned, err := h.probe().OwnedObjects(context.Background())
	if err != nil {
		t.Fatalf("the proof failed: %#v", diagnostics.Of(err))
	}
	want := []prerequisites.OwnedObject{
		{Kind: "libvirt domain", Name: "bootwright-lab-rhel-01"},
		{Kind: "libvirt domain", Name: "guest"},
		{Kind: "libvirt network", Name: "bootwright-lab-provisioning"},
		{Kind: "libvirt pool", Name: "media"},
		{Kind: "podman container", Name: "bootwright-lab-artifacts-web"},
		{Kind: "quadlet source", Name: quadletDirectory + "/bootwright-lab-dns-server-dns.container"},
		{Kind: "systemd unit", Name: "bootwright-lab-bmc-rhel-01.service"},
		{Kind: "systemd unit", Name: "bootwright-lab-dns-server-dns.service"},
	}
	if !slices.Equal(owned, want) {
		t.Fatalf("owned = %+v\nwant %+v", owned, want)
	}
	if slices.Contains(h.ran, virsh("dumpxml --inactive -- bootwright-lab-rhel-01")) {
		t.Fatalf("a prefixed name was read again: %q", h.ran)
	}
}

// A host without virsh, libvirt sockets, podman or quadlet sources, and with
// no unit listed, holds nothing owned, also when systemctl answers a pattern
// no unit file matches with exit 1 and no output at all, as systemd does.
func TestAHostWithoutLibvirtOrPodmanHoldsNothingOwned(t *testing.T) {
	for name, files := range map[string]result{"exit 0": {}, "silent exit 1": {code: 1}} {
		t.Run(name, func(t *testing.T) {
			h := &host{missing: []string{virshPath, podmanPath}, answers: map[string]result{units("list-unit-files"): files}}
			owned, err := h.probe().OwnedObjects(context.Background())
			if err != nil || len(owned) != 0 {
				t.Fatalf("owned = %+v (%#v)", owned, diagnostics.Of(err))
			}
			if !slices.Equal(h.ran, []string{units("list-units"), units("list-unit-files")}) {
				t.Fatalf("ran = %q", h.ran)
			}
		})
	}
}

// Every probe fails closed: a missing virsh while a libvirt socket exists, an
// unqualified executable, a missing systemctl, a non-zero exit, including a
// unit-file listing's exit 1 with an error or output and a loaded-unit
// listing's silent exit 1, a name that is
// not printable, a unit listing naming another unit and XML that does not
// parse each refuse rather than report nothing.
func TestTheProofRefusesWhatItCannotRead(t *testing.T) {
	cases := map[string]struct {
		host    host
		message string
	}{
		"virsh missing beside a libvirt socket": {host{missing: []string{virshPath}, socket: true}, "a libvirt socket exists under /run/libvirt"},
		"an unqualified virsh":                  {host{foreign: []string{virshPath}}, virshPath + " is not a root-owned system executable"},
		"an unqualified podman":                 {host{missing: []string{virshPath}, foreign: []string{podmanPath}}, podmanPath + " is not a root-owned"},
		"systemctl missing":                     {host{missing: []string{virshPath, systemctlPath}}, systemctlPath + " is missing"},
		"a failing virsh": {host{answers: map[string]result{virsh("list --all --name"): {code: 1, stderr: "error: failed to connect to the hypervisor\n"}}},
			"exited 1: error: failed to connect to the hypervisor"},
		"an unprintable name": {host{answers: map[string]result{virsh("list --all --name"): out("guest\x1b[2J\n")}}, "not a list of names"},
		"unparseable XML": {host{answers: map[string]result{virsh("list --all --name"): out("guest\n"), virsh("dumpxml --inactive -- guest"): out("<domain><name>")}},
			"its XML cannot be parsed"},
		"a unit of another prefix": {host{missing: []string{virshPath}, answers: map[string]result{units("list-units"): out("sshd.service loaded active running OpenSSH\n")}},
			"not a list of units"},
		"unit files exiting 1 with an error": {host{missing: []string{virshPath}, answers: map[string]result{units("list-unit-files"): {code: 1, stderr: "Failed to list unit files: Access denied\n"}}},
			"exited 1: Failed to list unit files: Access denied"},
		"unit files exiting 1 with output": {host{missing: []string{virshPath}, answers: map[string]result{units("list-unit-files"): {code: 1, stdout: []byte("bootwright-lab-dns.service enabled enabled\n")}}},
			"list-unit-files --no-legend --no-pager --full -- bootwright-* exited 1"},
		"loaded units exiting 1 silently": {host{missing: []string{virshPath}, answers: map[string]result{units("list-units"): {code: 1}}},
			"list-units --all --plain --no-legend --no-pager --full -- bootwright-* exited 1"},
		"a failing podman": {host{missing: []string{virshPath}, answers: map[string]result{podmanPath + " ps --all --format {{.Names}}": {code: 125, stderr: "database is locked"}}},
			"podman container remains"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := test.host.probe().OwnedObjects(context.Background())
			refused(t, err, test.message)
		})
	}
	var unconfigured *Probe
	if _, err := unconfigured.OwnedObjects(context.Background()); err == nil {
		t.Fatal("an unconfigured proof answered")
	}
}

func TestNameAndUnitListingsParse(t *testing.T) {
	if names, ok := parseNames([]byte("\n a \nb\n\n")); !ok || !slices.Equal(names, []string{"a", "b"}) {
		t.Fatalf("names = %q, %v", names, ok)
	}
	if _, ok := parseNames([]byte(strings.Repeat("x\n", maxObjects+1))); ok {
		t.Fatal("a listing beyond its bound parsed")
	}
	if _, ok := parseUnits([]byte("bootwright-lab\n"), "bootwright-"); ok {
		t.Fatal("a unit name without a type parsed")
	}
	if units, ok := parseUnits([]byte("bootwright-a.service x\n\nbootwright-b.socket y\n"), "bootwright-"); !ok || len(units) != 2 {
		t.Fatalf("units = %q, %v", units, ok)
	}
}
