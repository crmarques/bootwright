package v1alpha1

import (
	"strings"
	"testing"
)

// A device path is rendered into installer directives and shell words, so a
// line break inside one would start a directive the declaration never made.
func TestDevicePathRejectsNewline(t *testing.T) {
	for name, value := range map[string]string{
		"trailing newline":    "/dev/sda\n",
		"embedded directive":  "/dev/sda\nclearpart --all --initlabel",
		"carriage return":     "/dev/sda\r",
		"newline in segment":  "/dev/disk/by-id/a\nb",
		"leading line break":  "\n/dev/sda",
		"vertical whitespace": "/dev/sda\v",
	} {
		t.Run(name, func(t *testing.T) {
			if ValidLexical("device-path", value) {
				t.Fatalf("%q was admitted", value)
			}
		})
	}
}

// Only letters, digits and the separators stable device names use are
// admitted, so no quote, space or shell metacharacter survives admission.
func TestDevicePathRejectsCharactersOutsideTheSafeSet(t *testing.T) {
	for name, value := range map[string]string{
		"space":          "/dev/disk/by-id/my disk",
		"tab":            "/dev/sd\ta",
		"single quote":   "/dev/sda'",
		"double quote":   `/dev/sda"`,
		"backslash":      `/dev/sd\a`,
		"command":        "/dev/$(reboot)",
		"backtick":       "/dev/`id`",
		"semicolon":      "/dev/sda;reboot",
		"glob":           "/dev/sd*",
		"equals":         "/dev/sda=1",
		"comma":          "/dev/sda,sdb",
		"NUL":            "/dev/sda\x00",
		"non-ASCII":      "/dev/sdá",
		"percent escape": "/dev/sd%61",
	} {
		t.Run(name, func(t *testing.T) {
			if ValidLexical("device-path", value) {
				t.Fatalf("%q was admitted", value)
			}
		})
	}
}

// A device path is a clean absolute descendant of /dev: no traversal, no
// empty or dot segment, and nothing outside that tree.
func TestDevicePathRejectsTraversalAndUncleanPaths(t *testing.T) {
	for name, value := range map[string]string{
		"parent escape":    "/dev/../etc/shadow",
		"parent segment":   "/dev/disk/../sda",
		"parent at end":    "/dev/disk/..",
		"parent only":      "/dev/..",
		"dot segment":      "/dev/./sda",
		"empty segment":    "/dev//sda",
		"trailing slash":   "/dev/sda/",
		"device root":      "/dev/",
		"device directory": "/dev",
		"relative":         "dev/sda",
		"outside /dev":     "/sda",
		"prefix only":      "/devices/sda",
		"empty":            "",
	} {
		t.Run(name, func(t *testing.T) {
			if ValidLexical("device-path", value) {
				t.Fatalf("%q was admitted", value)
			}
		})
	}
}

// The names operators actually select a disk by stay admitted, including the
// stable links udev publishes beneath /dev/disk.
func TestDevicePathAdmitsStableDeviceNames(t *testing.T) {
	for _, value := range []string{
		"/dev/sda",
		"/dev/vda",
		"/dev/nvme0n1",
		"/dev/disk/by-path/pci-0000:00:1f.2-ata-1",
		"/dev/disk/by-id/wwn-0x5000c500a1b2c3d4",
		"/dev/disk/by-id/scsi-SATA_Disk_1.0",
		"/dev/mapper/vg+root-lv_root",
	} {
		if !ValidLexical("device-path", value) {
			t.Fatalf("%q was refused", value)
		}
	}
}

// lexicalRows checks that rule admits every admitted value and refuses every
// refused one.
func lexicalRows(t *testing.T, rule string, admitted, refused []string) {
	t.Helper()
	for _, value := range admitted {
		if !ValidLexical(rule, value) {
			t.Errorf("%s refused %q", rule, value)
		}
	}
	for _, value := range refused {
		if ValidLexical(rule, value) {
			t.Errorf("%s admitted %q", rule, value)
		}
	}
}

// An HTTP(S) URL reaches native configuration verbatim, so it is written only
// in RFC 3986's own characters: no whitespace, control, non-ASCII or excluded
// printable character survives admission.
func TestAnHTTPURLIsWrittenInRFC3986Characters(t *testing.T) {
	refused := []string{
		"https://mirror.example.test/rhel 9",
		"https://mirror.example.test/x?a=b c",
		"https://mirror.example.test/x\u00a0y",
		"https://mirror.example.test/x\u0085y",
		"https://mirror.example.test/x\u2028y",
		"https://mirror.example.test/x\u2029y",
		"https://mirror.example.test/x\u200by",
		"https://mirror.example.test/x\ty",
		"https://mirror.example.test/x\x7fy",
		"https://mirror.example.test/m\u00fcnchen",
	}
	for _, excluded := range "\"<>\\^`{|}" {
		refused = append(refused, "https://mirror.example.test/x"+string(excluded)+"y")
	}
	admitted := []string{
		"https://mirror.example.test/rhel9/BaseOS",
		"https://[2001:db8::1]/x",
		"https://mirror.example.test/x?a=b&c=d",
		"https://mirror.example.test/x#frag",
		"https://mirror.example.test/a%20b",
	}
	lexicalRows(t, "http-url", append(admitted, "http://192.0.2.1:8080/os/x/tree"), refused)
	lexicalRows(t, "https-url", admitted, append(refused, "http://192.0.2.1:8080/os/x/tree"))
}

// A proxy endpoint names where every acquisition goes and nothing else: an
// http or https URL with a host, no credential and nothing after its authority.
func TestAProxyEndpointIsABareHTTPAuthority(t *testing.T) {
	lexicalRows(t, "proxy-endpoint",
		[]string{"http://proxy.example.test:3128", "https://proxy.example.test:8443/", "http://192.0.2.10:3128", "http://[2001:db8::1]:3128", "http://proxy"},
		[]string{"http://proxy.example.test:3128/path", "http://proxy.example.test:3128/?q=1", "http://proxy.example.test:3128/#x",
			"http://user@proxy.example.test:3128", "http://user:secret@proxy.example.test:3128", "socks5://proxy.example.test:1080",
			"mailto:proxy@example.test", "proxy.example.test:3128", "http:///", "http://proxy.example.test:3128/é",
			" http://proxy.example.test:3128", "http://" + strings.Repeat("a", 4096) + ".test", ""})
}

// A proxy bypass entry matches destinations without a resolver, so it is a
// wildcard, an address, a block or a name, optionally with a port, and never a
// URL fragment.
func TestAProxyBypassEntryIsAHostAddressOrBlock(t *testing.T) {
	lexicalRows(t, "proxy-bypass",
		[]string{"*", "10.0.0.0/8", "2001:db8::/32", "192.0.2.7", "2001:db8::1", "lab.example.test", ".example.test", "*.example.test",
			"registry.example.test:443", "192.0.2.7:8080", "[2001:db8::1]:443", "LAB.Example.TEST"},
		[]string{"10.0.0.0/33", "lab.example.test/path", "user@lab.example.test", "lab.example.test?", "lab.example.test#", "lab example",
			"lab.example.test.", "*.", ".", "**", "lab.example.test:", "lab.example.test:https", "a:1:2", "[2001:db8::1]", "[2001:db8::1]443",
			"lab_example.test", " lab.example.test", "http://lab.example.test", ""})
}

// A repository base URL reaches a Kickstart command line, where '#' ends the
// line and a quote re-tokenizes it.
func TestARepositoryURLRefusesAFragmentOrAQuote(t *testing.T) {
	lexicalRows(t, "repository-url",
		[]string{"https://mirror.example.test/extras", "http://192.0.2.1:8080/extras?arch=x86_64"},
		[]string{"https://mirror.example.test/x#frag", "https://mirror.example.test/x#", "https://mirror.example.test/it's", `https://mirror.example.test/a"b`})
}

func TestASystemdUnitNameIsOneToken(t *testing.T) {
	lexicalRows(t, "systemd-unit",
		[]string{"chronyd", "sshd", "qemu-guest-agent", "cockpit.socket", "kdump", "getty@tty1.service", "dev-sda1.device", "a:b_c", strings.Repeat("a", 255)},
		[]string{"sshd\n%post", "my service", "a,b", "a#b", `a\x2db`, "a'b", `a"b`, "-sshd", ".sshd", "%post", "a/b", "a$b", "s\u00fcd", strings.Repeat("a", 256)})
}

func TestAPackageEntryIsAPackageSpec(t *testing.T) {
	lexicalRows(t, "package-spec",
		[]string{"chrony", "kernel-*", "*-devel", "@container-tools", "@^minimal-environment", "@nodejs:18/common", "bash-5.1.8-9.el9.x86_64", "libstdc++", "tar?", "glibc-langpack-[a-z][a-z]", "vim-enhanced-2:8.2~rc1^git", "_x"},
		[]string{"%end", "%post", "-kernel", "a#b", "touch${IFS}/x", "a,b", "bad package", "a'b", `a"b`, `a\b`, "a{b}", "p\u00e4ck", "a\nb", ".x"})
}

func TestAKickstartTokenCarriesNoSeparator(t *testing.T) {
	lexicalRows(t, "kickstart-token",
		[]string{"en_US.UTF-8", "sr_RS@latin", "us", "de-latin1-nodeadkeys", "Etc/UTC", "America/Port-au-Prince", "a%b"},
		[]string{"%post", "us#x", "en_US,de_DE", "a b", "a'b", `a"b`, `a\b`, "a\tb", "a\u00a0b", "\u00e9", "a\x7fb", "a\nb", "a\u2028b"})
}

func TestAnInterfaceNameIsALinuxInterfaceName(t *testing.T) {
	lexicalRows(t, "ifname",
		[]string{strings.Repeat("e", 15), "enp1s0", "eno1", "eth0", "bond0.100", "br-ceph-public", "a+b_c"},
		[]string{strings.Repeat("e", 16), "eth0/1", "eth0:1", ".", "..", "eth0\n%post", "eth 0", "\u00ebth0", "eth0'", `eth0"`})
}

// A bridge name is an interface name without the `+` firewalld reads as an
// interface wildcard, because a managed network puts its bridge in zone
// trusted by name, which would pull every matching interface into that zone.
func TestABridgeNameRefusesFirewalldsWildcard(t *testing.T) {
	lexicalRows(t, "bridge",
		[]string{strings.Repeat("b", 15), "virbr0", "virbr-lab", "br_lab.10"},
		[]string{"virbr+", "+", "a+b", strings.Repeat("b", 16), ".", "..", "eth0/1", "eth 0"})
}

const lexicalDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// A repository path component follows the OCI distribution grammar, so
// admission refuses what a container runtime would refuse to pull.
func TestImageAndRegistryPathsFollowTheOCIGrammar(t *testing.T) {
	refusedComponents := []string{"Bad", "a+b", "a!b", "-a", "a..b", "a_-b", "a___b", "a.", "_a"}
	admittedComponents := []string{"a.b", "a_b", "a__b", "a--b", "ocp-v4.0-art-dev", "ubi9"}
	for _, rule := range []string{"registry", "registry-base"} {
		admitted, refused := []string{"registry.example.test:5000", "registry.example.test"}, []string{}
		for _, component := range admittedComponents {
			admitted = append(admitted, "registry.example.test/team/"+component)
		}
		for _, component := range refusedComponents {
			refused = append(refused, "registry.example.test/team/"+component)
		}
		lexicalRows(t, rule, admitted, refused)
	}
	admitted, refused := []string{"registry.example.test/squid:V6.10-Beta_1", "registry.example.test/squid@SHA256:" + strings.ToUpper(lexicalDigest[7:])}, []string{}
	for _, component := range admittedComponents {
		admitted = append(admitted, "registry.example.test/"+component+":1", "registry.example.test/"+component+"@"+lexicalDigest)
	}
	for _, component := range refusedComponents {
		refused = append(refused, "registry.example.test/"+component+":1", "registry.example.test/"+component+"@"+lexicalDigest)
	}
	lexicalRows(t, "image", admitted, append(refused, "registry.example.test/squid:latest"))
}

// A download mirror is an HTTPS base URL the controller stage appends a
// release path to, so nothing after the path and no escape in it survives.
func TestMirrorURLsAreHTTPSBaseURLs(t *testing.T) {
	lexicalRows(t, "mirror-url",
		[]string{"https://mirror.example.test/helm", "https://mirror.example.test:443/helm", "https://mirror.example.test", "https://192.0.2.1/tools/", "https://[2001:db8::1]/tools"},
		[]string{"http://mirror.example.test/helm", "https://mirror.example.test:8443/helm", "https://mirror.example.test/helm?q", "https://mirror.example.test/helm?",
			"https://mirror.example.test/helm#f", "https://mirror.example.test/helm#", "https://user@mirror.example.test/helm", "https://mirror.example.test/a%2Fb",
			"https://mirror.example.test/" + strings.Repeat("a", 4096), "https://Mirror.example.test/helm", "https://mirror.example.test/rhel 9", ""})
}

func TestCanonicalImageLowercasesOnlyTheDigest(t *testing.T) {
	upper := "SHA256:" + strings.ToUpper(lexicalDigest[7:])
	for value, want := range map[string]string{
		"registry.example.test/squid@" + upper:              "registry.example.test/squid@" + lexicalDigest,
		"registry.example.test:5000/squid@" + lexicalDigest: "registry.example.test:5000/squid@" + lexicalDigest,
		"registry.example.test/squid:V6.10-Beta":            "registry.example.test/squid:V6.10-Beta",
		"Registry.Example.Test/Squid:V1":                    "Registry.Example.Test/Squid:V1",
	} {
		if got := CanonicalImage(value); got != want {
			t.Errorf("CanonicalImage(%q) = %q, want %q", value, got, want)
		}
	}
}
