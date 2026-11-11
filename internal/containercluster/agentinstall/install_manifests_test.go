package agentinstall

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func TestInstallNetworkManifestGolden(t *testing.T) {
	cluster := api.NewObject(api.OpenShiftCluster, "cluster", api.MapValue(), api.MapValue(api.FieldValue{Name: "networking", Value: api.MapValue(api.FieldValue{Name: "ovnKubernetesConfig", Value: api.MapValue(api.FieldValue{Name: "mtu", Value: api.IntegerValue("1400")}, api.FieldValue{Name: "gatewayConfig", Value: api.MapValue(api.FieldValue{Name: "routingViaHost", Value: api.BoolValue(true)}, api.FieldValue{Name: "ipForwarding", Value: api.StringValue("Global")})})})}))
	manifests, err := installManifests(api.NewCatalog(nil), cluster, []string{"198.51.100.2"})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateInstallManifests(manifests); err != nil {
		t.Fatal(err)
	}
	data, err := reconciliation.Freeze(map[string]any{"manifests": manifests}, "install manifests")
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "install-network-manifest", data)
	original := MediaRequest{Version: mediaRequestVersion, Manifests: manifests}
	before, _ := original.Canonical()
	original.Manifests = nil
	after, _ := original.Canonical()
	if string(before) == string(after) {
		t.Fatal("manifest absent from frozen identity")
	}
	manifests[0].File = "../escape.yaml"
	if validateInstallManifests(manifests) == nil {
		t.Fatal("escaping manifest accepted")
	}
}

const wantChronyConfig = "server 192.0.2.1 iburst\ndriftfile /var/lib/chrony/drift\nmakestep 1.0 3\nrtcsync\nlogdir /var/log/chrony\n"

func withTimeServers(catalog api.Catalog, selections api.Value) api.Catalog {
	objects := catalog.Objects()
	for i, object := range objects {
		if object.Kind() == api.OpenShiftCluster {
			objects[i] = object.WithSpec(object.Spec().WithPath(selections, "install", "ntp"))
		}
	}
	return api.NewCatalog(objects)
}

// The rendered YAML the media role writes is the frozen content, so the golden
// holds each manifest as a YAML document and reading it back yields the content.
func matchesYAMLGolden(t *testing.T, name string, manifests []InstallManifest) {
	t.Helper()
	var rendered bytes.Buffer
	for _, m := range manifests {
		document, err := yaml.Marshal(m.Content)
		if err != nil {
			t.Fatal(err)
		}
		rendered.WriteString("# " + m.File + "\n---\n")
		rendered.Write(document)
		var back map[string]any
		if err := yaml.Unmarshal(document, &back); err != nil {
			t.Fatal(err)
		}
		want, _ := json.Marshal(m.Content)
		got, _ := json.Marshal(back)
		if !bytes.Equal(got, want) {
			t.Fatalf("%s does not read back:\n%s\n%s", m.File, got, want)
		}
	}
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, rendered.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run with -update to write it", err)
	}
	if !bytes.Equal(rendered.Bytes(), want) {
		t.Fatalf("%s differs from its golden:\n%s", name, rendered.Bytes())
	}
}

func chronyConfigOf(t *testing.T, m InstallManifest) string {
	t.Helper()
	data, _ := json.Marshal(m.Content)
	var content struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
		Spec struct {
			Config struct {
				Ignition struct {
					Version string `json:"version"`
				} `json:"ignition"`
				Storage struct {
					Files []struct {
						Path      string `json:"path"`
						Mode      int    `json:"mode"`
						Overwrite bool   `json:"overwrite"`
						Contents  struct {
							Source string `json:"source"`
						} `json:"contents"`
					} `json:"files"`
				} `json:"storage"`
			} `json:"config"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(data, &content); err != nil {
		t.Fatal(err)
	}
	files := content.Spec.Config.Storage.Files
	if content.Spec.Config.Ignition.Version != "3.2.0" || len(files) != 1 || files[0].Path != "/etc/chrony.conf" || files[0].Mode != 0o644 || !files[0].Overwrite {
		t.Fatalf("%s = %s", m.File, data)
	}
	encoded, found := strings.CutPrefix(files[0].Contents.Source, "data:text/plain;charset=utf-8;base64,")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if !found || err != nil {
		t.Fatalf("%s source = %q", m.File, files[0].Contents.Source)
	}
	return string(decoded)
}

// Under the Environment's image mirror a cluster that selects a time server
// replaces each pool's chrony configuration with one naming those servers
// alone, and keeps them as the agent's additional sources too.
func TestTheImageMirrorReplacesTheNodesTimePool(t *testing.T) {
	media, _, _ := onlyRequests(t, environmentMirrorCatalog())
	if len(media.Manifests) != 2 {
		t.Fatalf("manifests = %+v", media.Manifests)
	}
	data, err := reconciliation.Freeze(map[string]any{"manifests": media.Manifests}, "install manifests")
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "install-chrony-manifests", data)
	matchesYAMLGolden(t, "install-chrony-manifests-yaml", media.Manifests)
	for index, role := range []string{"master", "worker"} {
		m := media.Manifests[index]
		if m.Kind != "MachineConfig" || m.Name != "99-"+role+"-bootwright-chrony" || m.Content["metadata"].(map[string]any)["labels"].(map[string]any)["machineconfiguration.openshift.io/role"] != role {
			t.Fatalf("manifest %d = %+v", index, m)
		}
		if config := chronyConfigOf(t, m); config != wantChronyConfig || strings.Contains(config, "pool ") {
			t.Fatalf("%s chrony.conf = %q", role, config)
		}
	}
	if sources, _ := media.AgentConfig["additionalNTPSources"].([]any); !slices.Equal(sources, []any{"192.0.2.1"}) {
		t.Fatalf("additionalNTPSources = %v", media.AgentConfig["additionalNTPSources"])
	}
	canonical, _ := media.Canonical()
	decoded, err := DecodeMediaRequest(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := decoded.Canonical(); !bytes.Equal(again, canonical) {
		t.Fatal("the frozen chrony manifests do not survive a round trip")
	}
}

// Every OpenShift topology has both pools, a single node or a compact cluster
// included, so each gets both manifests and a node joined to the worker pool
// later follows the same servers.
func TestEveryTopologyGetsBothPoolsChronyConfiguration(t *testing.T) {
	for name, catalog := range map[string]api.Catalog{"single": singleNodeCatalog(), "compact": compactCatalog()} {
		t.Run(name, func(t *testing.T) {
			media, _, _ := onlyRequests(t, underTheImageMirror(catalog))
			if len(media.Manifests) != 2 || media.Manifests[0].Name != "99-master-bootwright-chrony" || media.Manifests[1].Name != "99-worker-bootwright-chrony" {
				t.Fatalf("manifests = %+v", media.Manifests)
			}
		})
	}
}

// Without the image mirror, or with no time server selected, no manifest is
// written and the request is the one earlier builds froze.
func TestNoChronyConfigurationWithoutTheMirrorOrATimeServer(t *testing.T) {
	cases := map[string]api.Catalog{
		"no mirror":      singleNodeCatalog(),
		"no time server": withTimeServers(environmentMirrorCatalog(), api.ListValue()),
	}
	for name, catalog := range cases {
		t.Run(name, func(t *testing.T) {
			media, _, _ := onlyRequests(t, catalog)
			if len(media.Manifests) != 0 {
				t.Fatalf("manifests = %+v", media.Manifests)
			}
		})
	}
}

func TestAMalformedTimeServerAddressRefusesUnderTheMirror(t *testing.T) {
	catalog := withTimeServers(environmentMirrorCatalog(api.NewObject(api.NTPServer, "outside", api.Value{}, api.MapValue(text("management", "external"), text("address", "ntp.example.test\npool 0.pool.ntp.org")))),
		api.ListValue(api.MapValue(text("serverRef", "outside"))))
	if _, _, _, err := Requests(catalog, "controller", testContext); err == nil || !strings.Contains(diagnostics.Of(err)[0].Message, "is not a host name or IP address") {
		t.Fatalf("a malformed time server address was accepted: %v", err)
	}
}

// The manifest path admits the chrony MachineConfig only in the exact form
// Bootwright derives, never another MachineConfig or an edited one.
func TestOnlyTheDerivedChronyMachineConfigIsAdmitted(t *testing.T) {
	derived := func() []InstallManifest {
		return []InstallManifest{chronyManifest("master", []string{"192.0.2.1", "ntp.example.test"}), chronyManifest("worker", []string{"192.0.2.1", "ntp.example.test"})}
	}
	if err := validateInstallManifests(derived()); err != nil {
		t.Fatal(err)
	}
	source := func(config string) map[string]any {
		return map[string]any{"source": "data:text/plain;charset=utf-8;base64," + base64.StdEncoding.EncodeToString([]byte(config))}
	}
	file := func(m InstallManifest) map[string]any {
		return m.Content["spec"].(map[string]any)["config"].(map[string]any)["storage"].(map[string]any)["files"].([]any)[0].(map[string]any)
	}
	tampered := map[string]func(m *InstallManifest){
		"a pool line": func(m *InstallManifest) {
			file(*m)["contents"] = source("pool 2.rhel.pool.ntp.org iburst\n" + chronyDefaults)
		},
		"no server": func(m *InstallManifest) { file(*m)["contents"] = source(chronyDefaults) },
		"an extra directive": func(m *InstallManifest) {
			file(*m)["contents"] = source("server 192.0.2.1 iburst\nallow all\n" + chronyDefaults)
		},
		"another file": func(m *InstallManifest) { file(*m)["path"] = "/etc/sysconfig/chronyd" },
		"another mode": func(m *InstallManifest) { file(*m)["mode"] = 493 },
		"another role label": func(m *InstallManifest) {
			m.Content["metadata"].(map[string]any)["labels"] = map[string]any{"machineconfiguration.openshift.io/role": "worker"}
		},
		"another name": func(m *InstallManifest) {
			m.Name = "99-infra-bootwright-chrony"
			m.Content["metadata"].(map[string]any)["name"] = m.Name
		},
		"another file name":   func(m *InstallManifest) { m.File = "bootwright-12-master-chrony.yaml" },
		"an authored config":  func(m *InstallManifest) { m.Content["spec"].(map[string]any)["kernelArguments"] = []any{"nosmt"} },
		"another API version": func(m *InstallManifest) { m.APIVersion = "machineconfiguration.openshift.io/v2" },
	}
	for name, change := range tampered {
		t.Run(name, func(t *testing.T) {
			manifests := derived()
			change(&manifests[0])
			if validateInstallManifests(manifests) == nil {
				t.Fatal("a MachineConfig other than the derived one was admitted")
			}
		})
	}
}

func TestAnIPv6TimeServerIsNamedUnderTheMirror(t *testing.T) {
	catalog := withTimeServers(environmentMirrorCatalog(api.NewObject(api.NTPServer, "outside", api.Value{}, api.MapValue(text("management", "external"), text("address", "::ffff:192.0.2.1")))),
		api.ListValue(api.MapValue(text("serverRef", "outside"))))
	media, _, _ := onlyRequests(t, catalog)
	if len(media.Manifests) != 2 || !strings.HasPrefix(chronyConfigOf(t, media.Manifests[1]), "server ::ffff:192.0.2.1 iburst\ndriftfile ") {
		t.Fatalf("manifests = %+v", media.Manifests)
	}
	canonical, _ := media.Canonical()
	if _, err := DecodeMediaRequest(canonical); err != nil {
		t.Fatal(err)
	}
}

// A frozen request carries both pools' chrony configurations or neither, each
// naming exactly the agent's additional time sources.
func TestTheChronyManifestsAgreeWithEachOtherAndTheAgentConfiguration(t *testing.T) {
	cases := map[string]func(m *MediaRequest){
		"master alone": func(m *MediaRequest) { m.Manifests = m.Manifests[:1] },
		"worker alone": func(m *MediaRequest) { m.Manifests = m.Manifests[1:] },
		"other servers": func(m *MediaRequest) {
			m.Manifests = []InstallManifest{chronyManifest("master", []string{"192.0.2.9"}), chronyManifest("worker", []string{"192.0.2.9"})}
		},
		"one pool differs":   func(m *MediaRequest) { m.Manifests[1] = chronyManifest("worker", []string{"192.0.2.1", "192.0.2.9"}) },
		"no agent sources":   func(m *MediaRequest) { delete(m.AgentConfig, "additionalNTPSources") },
		"more agent sources": func(m *MediaRequest) { m.AgentConfig["additionalNTPSources"] = []any{"192.0.2.1", "192.0.2.9"} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			media, _, _ := onlyRequests(t, environmentMirrorCatalog())
			change(&media)
			canonical, err := media.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeMediaRequest(canonical); err == nil {
				t.Fatal("disagreeing chrony manifests were admitted")
			}
		})
	}
}
