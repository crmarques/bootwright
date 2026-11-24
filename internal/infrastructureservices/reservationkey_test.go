package infrastructureservices

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Reservation keys are stored in the workspace and hashed into plans, so the
// text form of every kind is pinned byte for byte.
func TestEveryReservationKeyKeepsItsStoredText(t *testing.T) {
	for name, test := range map[string]struct {
		key  Key
		text string
	}{
		"a controller with its port":            {ControllerReservationKey("https://BMC.example:8443/redfish/v1/Systems/abc"), "bmc:BMC.example:8443/abc"},
		"a controller at the HTTPS default":     {ControllerReservationKey("https://bmc.example/redfish/v1/Systems/abc"), "bmc:bmc.example:443/abc"},
		"a controller at the HTTP default":      {ControllerReservationKey("http://[fd00::1]/redfish/v1/Systems/x"), "bmc:[fd00::1]:80/x"},
		"an unparsed controller":                {ControllerReservationKey("http://[::1"), "bmc:http://[::1"},
		"a controller's media":                  {ControllerMediaReservationKey("https://bmc.example/redfish/v1/Systems/abc"), "bmc-media:bmc.example:443"},
		"an unparsed controller's media":        {ControllerMediaReservationKey("http://[::1"), "bmc-media:http://[::1"},
		"a system UUID":                         {SystemReservationKey(" ABC-Def "), "bmc-system:abc-def"},
		"a MAC address":                         {MACReservationKey(" AA-BB-CC-DD-EE-FF "), "bmc-mac:aa:bb:cc:dd:ee:ff"},
		"an unparsed MAC address":               {MACReservationKey("Zz"), "bmc-mac:zz"},
		"an image":                              {MediaReservationKey("rhel.iso"), "media:rhel.iso"},
		"a path":                                {PathReservationKey("/srv/artifacts"), "path:/srv/artifacts"},
		"a unit":                                {UnitReservationKey("bootwright-dns"), "unit:bootwright-dns"},
		"a socket":                              {SocketReservationKey("::", "53"), "socket::::53"},
		"a bridge":                              {BridgeReservationKey("virbr-lab"), "bridge:virbr-lab"},
		"a bridge in use":                       {BridgeUseReservationKey("br-shared"), "bridge-use:br-shared"},
		"a prefix":                              {PrefixReservationKey(netip.MustParsePrefix("203.0.113.193/26")), "prefix:203.0.113.192/26"},
		"a NetworkManager connection":           {ConnectionReservationKey("trunk-160"), "nm-connection:trunk-160"},
		"a libvirt pool":                        {LibvirtPoolReservationKey("lab-pool"), "libvirt-pool:lab-pool"},
		"a libvirt network":                     {LibvirtNetworkReservationKey("lab-net"), "libvirt-network:lab-net"},
		"a libvirt domain":                      {LibvirtDomainReservationKey("lab-node-01"), "libvirt-domain:lab-node-01"},
		"a key qualified by an SSH address":     {Key{Qualifier: SSHReservationQualifier("FD00::0001", 2222), Class: "socket", Subject: "0.0.0.0:3128"}, "ssh/fd00::1:2222/socket:0.0.0.0:3128"},
		"a key qualified by a pinned host key":  {Key{Qualifier: pinnedQualifier(t), Class: "unit", Subject: "x"}, "sshkey/SHA256:gNSIRW-2Iyiuvsdp_bgjy38bvWHw6wQm3tuoXrl3WjQ/unit:x"},
		"a key on the controller":               {Key{Class: "path", Subject: "/srv"}, "path:/srv"},
		"a mapped IPv4 SSH address qualifier":   {Key{Qualifier: SSHReservationQualifier("::ffff:192.0.2.1", 22), Class: "unit", Subject: "x"}, "ssh/192.0.2.1:22/unit:x"},
		"a DNS name SSH address qualifier":      {Key{Qualifier: SSHReservationQualifier("Host.Example.", 22), Class: "unit", Subject: "x"}, "ssh/host.example:22/unit:x"},
		"a controller key on an SSH-placed run": {Key{Qualifier: SSHReservationQualifier("192.0.2.1", 22), Class: "bmc", Subject: "bmc.example:443/1"}, "ssh/192.0.2.1:22/bmc:bmc.example:443/1"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := test.key.String(); got != test.text {
				t.Fatalf("key text = %q, want %q", got, test.text)
			}
			if parsed, ok := ParseKey(test.text); !ok || parsed != test.key {
				t.Fatalf("ParseKey(%q) = %+v, %v, want %+v", test.text, parsed, ok, test.key)
			}
		})
	}
	if got := QualifiedKeys(pinnedQualifier(t), []string{"socket:0.0.0.0:3128", "unit:x"}); !slices.Equal(got, []string{
		"sshkey/SHA256:gNSIRW-2Iyiuvsdp_bgjy38bvWHw6wQm3tuoXrl3WjQ/socket:0.0.0.0:3128",
		"sshkey/SHA256:gNSIRW-2Iyiuvsdp_bgjy38bvWHw6wQm3tuoXrl3WjQ/unit:x",
	}) {
		t.Fatalf("qualified keys = %q", got)
	}
	if got := QualifiedKeys(Qualifier{}, []string{"unit:x"}); !slices.Equal(got, []string{"unit:x"}) {
		t.Fatalf("a controller claim's keys were qualified: %q", got)
	}
	if MaxReservationKeys != 64 || MaxPublishedReservationKeys != 128 || MaxReservationKeyBytes != 256 || SSHHostKeyQualifierBytes != 58 {
		t.Fatalf("bounds = %d %d %d %d", MaxReservationKeys, MaxPublishedReservationKeys, MaxReservationKeyBytes, SSHHostKeyQualifierBytes)
	}
	if _, ok := HostKeyQualifier(""); ok {
		t.Fatal("an unreadable host key fingerprint qualified a key")
	}
	if _, ok := ParseKey("classless"); ok {
		t.Fatal("text with no class read as a key")
	}
}

func pinnedQualifier(t *testing.T) Qualifier {
	t.Helper()
	qualifier, ok := HostKeyQualifier("SHA256:gNSIRW+2Iyiuvsdp/bgjy38bvWHw6wQm3tuoXrl3WjQ")
	if !ok {
		t.Fatal("the fingerprint qualified no key")
	}
	return qualifier
}

// One physical machine is one claim, whichever spelling of its endpoint the
// declaration used, so two contexts cannot both drive it.
func TestOneControllerIsOneClaimHoweverItIsSpelled(t *testing.T) {
	for address, key := range map[string]string{
		"https://bmc.example.test/redfish/v1/Systems/1":              "bmc:bmc.example.test:443/1",
		"http://bmc.example.test/redfish/v1/Systems/1":               "bmc:bmc.example.test:80/1",
		"https://bmc.example.test:8443/redfish/v1/Systems/1":         "bmc:bmc.example.test:8443/1",
		"https://[2001:db8::1]/redfish/v1/Systems/System.Embedded.1": "bmc:[2001:db8::1]:443/System.Embedded.1",
	} {
		t.Run(address, func(t *testing.T) {
			if got := ControllerReservationKey(address).String(); got != key {
				t.Fatalf("key = %q, want %q", got, key)
			}
		})
	}
}

// The key table of specs/infrastructure-services.md#host-reservations names
// every class a *Class constant of reservationkey.go declares, and each of its
// rows names a declared class or one a capability builds without a constant.
func TestHostReservationKeyTableNamesEveryKeyClass(t *testing.T) {
	declared := slices.Clone(keyClassesWithoutAConstant)
	file, err := parser.ParseFile(token.NewFileSet(), "reservationkey.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		for _, spec := range group.Specs {
			value := spec.(*ast.ValueSpec)
			for index, name := range value.Names {
				if literal, ok := value.Values[index].(*ast.BasicLit); ok && strings.HasSuffix(name.Name, "Class") {
					class, _ := strconv.Unquote(literal.Value)
					declared = append(declared, class)
				}
			}
		}
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "specs", "infrastructure-services.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, table, _ := strings.Cut(string(data), "A reservation key is one of:")
	table, _, _ = strings.Cut(table, "A `socket:` key writes")
	var rows []string
	for _, match := range regexp.MustCompile("(?m)^\\| `([a-z-]+):").FindAllStringSubmatch(table, -1) {
		rows = append(rows, match[1])
	}
	slices.Sort(rows)
	slices.Sort(declared)
	if !slices.Equal(rows, declared) {
		t.Fatalf("the key table names the classes %q, want %q (the *Class constants of reservationkey.go and %q)", rows, declared, keyClassesWithoutAConstant)
	}
}

var keyClassesWithoutAConstant = []string{"bmc", "bmc-media", "kubevirt-vm"}
