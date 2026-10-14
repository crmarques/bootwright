package substrate

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// A generated serving certificate of an emulated BMC names its bindAddress,
// compared as Go's hostname verification compares an IP. A stored one, a
// missing or mistyped Secret, and an unnameable address are another rule's.
func TestAGeneratedBMCCertificateNamesItsBindAddress(t *testing.T) {
	const field = "$.spec.libvirt.bmcEmulationDefaults.certificateRef"
	host := obj(api.Machine, "host", m("capabilities", api.StringList("libvirt")))
	provider := func(address string) api.Object {
		o, _ := Normalize(obj(api.InfraProvider, "lab", m("libvirt", m("machineRef", "host", "uri", "qemu:///system",
			"bmcEmulationDefaults", m("port", api.IntegerValue("8000"), "bindAddress", address, "certificateRef", "bmc-tls", "auth", m("credentialsRef", "bmc"))))), api.Catalog{})
		return o
	}
	generated := func(ips ...string) api.Object {
		return obj(api.Secret, "bmc-tls", m("type", "tlsCertificate", "source", m("generated", m("commonName", "localhost", "dnsNames", api.StringList("localhost"), "ipAddresses", api.StringList(ips...)))))
	}
	stored := obj(api.Secret, "bmc-tls", m("type", "tlsCertificate", "source", m("contextStore", m())))
	mistyped := obj(api.Secret, "bmc-tls", m("type", "usernamePassword", "source", m("generated", m("username", "admin"))))
	for name, test := range map[string]struct {
		address string
		secret  *api.Object
		refused bool
	}{
		"an IPv4 address it names":             {"192.0.2.1", ptr(generated("192.0.2.1")), false},
		"an IPv6 address in another spelling":  {"2001:DB8:0::1", ptr(generated("2001:db8:0:0::1")), false},
		"an IPv4 address its mapped SAN names": {"192.0.2.1", ptr(generated("::ffff:192.0.2.1")), false},
		"an address it does not name":          {"192.0.2.1", ptr(generated("127.0.0.1")), true},
		"an address only a DNS name could":     {"127.0.0.1", ptr(generated()), true},
		"a stored certificate":                 {"192.0.2.1", &stored, false},
		"a Secret of another type":             {"192.0.2.1", &mistyped, false},
		"a missing Secret":                     {"192.0.2.1", nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			o := provider(test.address)
			objects := []api.Object{o, host}
			if test.secret != nil {
				objects = append(objects, *test.secret)
			}
			issues := Validate(o, api.NewCatalog(objects))
			if !test.refused {
				if len(issues) != 0 {
					t.Fatal(issues)
				}
				return
			}
			address := o.Spec().Get("libvirt", "bmcEmulationDefaults", "bindAddress").Text()
			want := api.Issue{Code: "api.invariant", Field: field,
				Message: "the emulated BMC serves HTTPS at " + address + ", which the generated certificate of Secret/bmc-tls does not name",
				Remediation: "add " + address + " to spec.source.generated.ipAddresses of Secret/bmc-tls, import the change with bootwright context update --name <context> --input-dir <dir>, " +
					"then run bootwright secret generate --name bmc-tls --context <context>"}
			if len(issues) != 1 || issues[0] != want {
				t.Fatalf("issues = %+v, want %+v", issues, want)
			}
		})
	}
	unnameable := provider("0.0.0.0")
	issues := Validate(unnameable, api.NewCatalog([]api.Object{unnameable, host, generated("192.0.2.1")}))
	if len(issues) != 1 || issues[0].Field != "$.spec.libvirt.bmcEmulationDefaults.bindAddress" {
		t.Fatalf("an unnameable address was judged beside its own refusal: %+v", issues)
	}
}

func ptr(o api.Object) *api.Object { return &o }
