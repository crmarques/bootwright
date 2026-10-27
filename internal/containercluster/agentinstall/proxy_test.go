package agentinstall

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func proxyCatalog(managed bool) api.Catalog {
	objects := singleNodeCatalog().Objects()
	for i, object := range objects {
		if object.Kind() == api.OpenShiftCluster {
			objects[i] = object.WithSpec(object.Spec().WithPath(api.MapValue(text("proxyRef", "egress"), field("noProxy", api.StringList("*.example.test", "[2001:db8::1]"))), "install", "proxy"))
		}
	}
	spec := api.MapValue(text("management", "external"), field("connection", api.MapValue(text("httpProxy", "http://proxy.example.test:3128"), field("auth", api.MapValue(text("proxyAuthRef", "proxy-auth"))))))
	if managed {
		spec = api.MapValue(text("management", "managed"), text("machineRef", "controller"), field("endpoints", api.ListValue(api.MapValue(text("name", "ip"), text("addressRef", "ip")))))
	}
	return api.NewCatalog(append(objects, api.NewObject(api.Proxy, "egress", api.Value{}, spec)))
}

func TestInstallationProxyGolden(t *testing.T) {
	for _, managed := range []bool{false, true} {
		name := "external"
		if managed {
			name = "managed"
		}
		t.Run(name, func(t *testing.T) {
			media, _, needs := onlyRequests(t, proxyCatalog(managed))
			data, err := media.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			matchesGolden(t, "media-request-proxy-"+name, data)
			if _, err := DecodeMediaRequest(data); err != nil {
				t.Fatal(err)
			}
			if managed && (len(needs.Proxies) != 1 || len(mediaRequires(needs)) != 2) {
				t.Fatalf("managed requirements: %+v", needs)
			}
			if !strings.Contains(media.InstallProxy.NoProxy, "198.51.100.0/24,.sno.lab.example.test") {
				t.Fatal(media.InstallProxy.NoProxy)
			}
		})
	}
}

func TestInstallerBypassTranslation(t *testing.T) {
	for input, want := range map[string]string{"*": "*", "*.example.test": ".example.test", "example.test": "example.test", "[2001:db8::1]": "2001:db8::1", "192.0.2.1": "192.0.2.1", "192.0.2.0/24": "192.0.2.0/24"} {
		got, err := installerBypass(input)
		if err != nil || got != want {
			t.Fatalf("%q: %q %v", input, got, err)
		}
	}
	for _, input := range []string{"example.test:443", "[2001:db8::1]:443", "192.0.2.1:80"} {
		if _, err := installerBypass(input); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestBuildEgressUsesControllerChoice(t *testing.T) {
	objects := proxyCatalog(false).Objects()
	for i, object := range objects {
		if object.Kind() == api.Machine && object.Name() == "controller" {
			objects[i] = object.WithSpec(object.Spec().With("proxy", api.MapValue(text("proxyRef", "build-egress"), field("noProxy", api.StringList("localhost")))))
		}
	}
	objects = append(objects, api.NewObject(api.Proxy, "build-egress", api.Value{}, api.MapValue(text("management", "external"), field("connection", api.MapValue(text("httpsProxy", "http://build.example.test:3128"))))))
	media, _, _ := onlyRequests(t, api.NewCatalog(objects))
	if media.BuildEgress["HTTPS_PROXY"] != "http://build.example.test:3128" || media.BuildEgress["HTTP_PROXY"] != media.BuildEgress["HTTPS_PROXY"] || media.BuildEgress["NO_PROXY"] != "localhost" {
		t.Fatalf("egress = %v", media.BuildEgress)
	}
	data, err := media.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "media-request-build-proxy", data)
}
