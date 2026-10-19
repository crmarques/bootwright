package bundlelocal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func TestEveryAdmittedCLIVersionPassesTheToolScreen(t *testing.T) {
	for _, value := range []string{"1.2.3", "v1.9.0-rc.0", strings.Repeat("1", 95), "v" + strings.Repeat("1", 95), strings.Repeat("1", 96), "v" + strings.Repeat("1", 96)} {
		if !api.ValidLexical("cli-version", value) {
			continue
		}
		requests := []controller.ToolRequest{{Kind: "helm", Version: "v" + strings.TrimPrefix(value, "v")}}
		if _, err := toolRequests(requests, func(string) bool { return true }); err != nil {
			t.Errorf("admitted %d-byte tag %q is refused by setup: %v", len(value), value, err)
		}
	}
}

func TestHelmLatestResolvesPublisherLockAndRetainsItWithoutNetwork(t *testing.T) {
	request := controller.ToolRequest{Kind: "helm", Version: "latest", Mirror: "https://mirror.example.test/tools"}
	digest := strings.Repeat("a", 64)
	requests := []string{}
	resolver := &ToolCatalog{metadata: func(_ context.Context, method, endpoint string, _ int64, egress prerequisites.SetupEgress) (toolMetadata, error) {
		requests = append(requests, method+" "+endpoint)
		if egress.HTTPSProxy != "https://proxy.example.test:3128" {
			t.Fatal("explicit route lost")
		}
		switch endpoint {
		case "https://api.github.com/repos/helm/helm/releases/latest":
			return toolMetadata{data: []byte(`{"tag_name":"v4.2.3","draft":false,"prerelease":false,"assets":[]}`)}, nil
		case "https://get.helm.sh/helm-v4.2.3-linux-amd64.tar.gz.sha256sum":
			return toolMetadata{data: []byte(digest + "  helm-v4.2.3-linux-amd64.tar.gz\n")}, nil
		case "https://get.helm.sh/helm-v4.2.3-linux-amd64.tar.gz":
			if method != http.MethodHead {
				t.Fatal("metadata resolver acquired payload")
			}
			return toolMetadata{size: 12345}, nil
		default:
			t.Fatal("unexpected source", endpoint)
			return toolMetadata{}, errors.New("unexpected request")
		}
	}}
	tools, err := resolver.Resolve(context.Background(), []controller.ToolRequest{request}, prerequisites.SetupEgress{HTTPSProxy: "https://proxy.example.test:3128"})
	if err != nil || len(tools) != 1 || len(requests) != 3 {
		t.Fatal(tools, err, requests)
	}
	tool := tools[0]
	if tool.Source.URL != "https://mirror.example.test/tools/helm-v4.2.3-linux-amd64.tar.gz" || tool.Source.SHA256 != digest || tool.Source.Bytes != 12345 || tool.Files[0].Member != "linux-amd64/helm" {
		t.Fatal(tool)
	}
	selected, complete, err := (&ToolCatalog{}).Select([]controller.ToolRequest{request}, []prerequisites.DependencySource{tool.Source})
	if err != nil || !complete || !reflect.DeepEqual(selected, tools) {
		t.Fatal(selected, complete, err)
	}
	selected[0].Files[0].Path = "mutated"
	again, _, _ := (&ToolCatalog{}).Select([]controller.ToolRequest{request}, []prerequisites.DependencySource{tool.Source})
	if again[0].Files[0].Path == "mutated" {
		t.Fatal("frozen tool shares mutable results")
	}
}

func TestToolMetadataRefusesUnsupportedCompatibilityBeforeNetwork(t *testing.T) {
	for _, request := range []controller.ToolRequest{{Kind: "virtctl", Version: "4.21.0", Compatibility: "openshift-virtualization"}, {Kind: "helm", Version: "latest", Mirror: "https://user:secret@example.test"}, {Kind: "openshift-install", Version: "latest", Compatibility: "openshift"}} {
		calls := 0
		resolver := &ToolCatalog{metadata: func(context.Context, string, string, int64, prerequisites.SetupEgress) (toolMetadata, error) {
			calls++
			return toolMetadata{}, nil
		}}
		if _, err := resolver.Resolve(context.Background(), []controller.ToolRequest{request}, prerequisites.SetupEgress{}); err == nil || calls != 0 {
			t.Fatal(request, err, calls)
		}
	}
}

func TestFrozenToolRejectsVersionRouteAndChecksumSubstitution(t *testing.T) {
	request := controller.ToolRequest{Kind: "openshift-clients", Version: "4.21.15", Compatibility: "openshift"}
	endpoint, _, _ := sourceURL(request, request.Version)
	source := prerequisites.DependencySource{ID: toolSourcePrefix(request) + request.Version, URL: endpoint, SHA256: strings.Repeat("b", 64), Bytes: 1024}
	for _, edit := range []func(*prerequisites.DependencySource){func(s *prerequisites.DependencySource) { s.URL = "https://other.example.test/oc.tar.gz" }, func(s *prerequisites.DependencySource) { s.SHA256 = strings.Repeat("B", 64) }, func(s *prerequisites.DependencySource) { s.Bytes = maxToolSourceBytes + 1 }, func(s *prerequisites.DependencySource) { s.ID = toolSourcePrefix(request) + "4.21.16" }} {
		changed := source
		edit(&changed)
		if _, _, err := (&ToolCatalog{}).Select([]controller.ToolRequest{request}, []prerequisites.DependencySource{changed}); err == nil {
			t.Fatal("changed retained source accepted", changed)
		}
	}
	selected, complete, err := (&ToolCatalog{}).Select([]controller.ToolRequest{request}, nil)
	if err != nil || complete || len(selected) != 0 {
		t.Fatal("missing lock became ready", selected, complete, err)
	}
}

func TestPublisherChecksumAndOriginBoundaries(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, input := range []string{digest + "  wanted\n" + digest + "  wanted\n", "bad  wanted\n", digest + "  unrelated\n", strings.ToUpper(digest) + "  wanted\n", digest + "\n# unrelated manifest\n"} {
		if _, err := toolChecksum([]byte(input), "wanted"); err == nil {
			t.Fatal("invalid checksum metadata accepted")
		}
	}
	for _, raw := range []string{"http://get.helm.sh/file", "https://get.helm.sh:444/file", "https://user:secret@get.helm.sh/file", "https://attacker.example.test/file"} {
		parsed, _ := url.Parse(raw)
		if toolMetadataOrigin(parsed) {
			t.Fatal("unapproved metadata origin accepted", raw)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resolver := &ToolCatalog{metadata: func(context.Context, string, string, int64, prerequisites.SetupEgress) (toolMetadata, error) {
		t.Fatal("canceled resolution read metadata")
		return toolMetadata{}, nil
	}}
	if _, err := resolver.Resolve(ctx, []controller.ToolRequest{{Kind: "helm", Version: "latest"}}, prerequisites.SetupEgress{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestFrozenLatestSelectsDeterministicallyAndRejectsConflictingOldIdentities(t *testing.T) {
	request := controller.ToolRequest{Kind: "helm", Version: "latest"}
	retained := []prerequisites.DependencySource{}
	for _, version := range []string{"v4.2.9", "4.2.10", "v4.2.10"} {
		endpoint, _, err := sourceURL(request, version)
		if err != nil {
			t.Fatal(err)
		}
		retained = append(retained, prerequisites.DependencySource{ID: toolSourcePrefix(request) + version, URL: endpoint, SHA256: strings.Repeat("a", 64), Bytes: 1024})
	}
	resolver := &ToolCatalog{metadata: func(context.Context, string, string, int64, prerequisites.SetupEgress) (toolMetadata, error) {
		t.Fatal("retained latest read mutable publisher metadata")
		return toolMetadata{}, nil
	}}
	selected, complete, err := resolver.Select([]controller.ToolRequest{request}, retained)
	if err != nil || !complete || len(selected) != 1 || selected[0].Version != "v4.2.10" {
		t.Fatal(selected, complete, err)
	}
	slices.Reverse(retained)
	reordered, complete, err := resolver.Select([]controller.ToolRequest{request}, retained)
	if err != nil || !complete || !reflect.DeepEqual(selected, reordered) {
		t.Fatal("retention order changed frozen release", reordered, err)
	}
	conflict := retained[len(retained)-1]
	conflict.SHA256 = strings.Repeat("b", 64)
	retained = append(retained, conflict)
	if _, _, err := resolver.Select([]controller.ToolRequest{request}, retained); err == nil {
		t.Fatal("conflicting older publisher identity was hidden by newest release")
	}
}

func TestOKDSelectsExactPublishedSCOSTagAndRetainsItsRepository(t *testing.T) {
	for _, mirror := range []string{"", "https://mirror.example.test/tools"} {
		t.Run(mirror, func(t *testing.T) {
			request := controller.ToolRequest{Kind: "openshift-clients", Version: "4.18.0-okd-scos.8", Compatibility: "okd", Mirror: mirror}
			publisher := "https://github.com/okd-project/okd-scos/releases/download/4.18.0-okd-scos.8/openshift-client-linux-4.18.0-okd-scos.8.tar.gz"
			digest := strings.Repeat("d", 64)
			calls := []string{}
			resolver := &ToolCatalog{metadata: func(_ context.Context, method, endpoint string, _ int64, _ prerequisites.SetupEgress) (toolMetadata, error) {
				if method != http.MethodGet {
					t.Fatal("unexpected payload size request")
				}
				calls = append(calls, endpoint)
				switch endpoint {
				case "https://api.github.com/repos/okd-project/okd/releases/tags/4.18.0-okd-scos.8":
					return toolMetadata{}, errToolMetadataNotFound
				case "https://api.github.com/repos/okd-project/okd-scos/releases/tags/4.18.0-okd-scos.8":
					return toolMetadata{data: []byte(fmt.Sprintf(`{"tag_name":"4.18.0-okd-scos.8","assets":[{"name":"openshift-client-linux-4.18.0-okd-scos.8.tar.gz","browser_download_url":%q,"size":1024,"digest":"sha256:%s"}]}`, publisher, digest))}, nil
				default:
					t.Fatal("unapproved metadata origin", endpoint)
					return toolMetadata{}, nil
				}
			}}
			resolved, err := resolver.Resolve(context.Background(), []controller.ToolRequest{request}, prerequisites.SetupEgress{})
			if err != nil || len(resolved) != 1 || len(calls) != 2 {
				t.Fatal(resolved, err, calls)
			}
			wanted := publisher
			if mirror != "" {
				wanted = mirror + "/4.18.0-okd-scos.8/openshift-client-linux-4.18.0-okd-scos.8.tar.gz"
			}
			if resolved[0].Source.URL != wanted || resolved[0].Source.SHA256 != digest {
				t.Fatal("publisher identity or explicit mirror lost", resolved)
			}
			selected, complete, err := (&ToolCatalog{}).Select([]controller.ToolRequest{request}, []prerequisites.DependencySource{resolved[0].Source})
			if err != nil || !complete || !reflect.DeepEqual(selected, resolved) {
				t.Fatal("SCOS publisher lock was not retained", selected, complete, err)
			}
		})
	}
}

func TestOKDRepositoryFallbackRequiresDefinitiveNotFound(t *testing.T) {
	want := errors.New("publisher refused metadata")
	calls := 0
	resolver := &ToolCatalog{metadata: func(context.Context, string, string, int64, prerequisites.SetupEgress) (toolMetadata, error) {
		calls++
		return toolMetadata{}, want
	}}
	_, err := resolver.Resolve(context.Background(), []controller.ToolRequest{{Kind: "openshift-install", Version: "4.18.0-okd-scos.8", Compatibility: "okd"}}, prerequisites.SetupEgress{})
	if !errors.Is(err, want) || calls != 1 {
		t.Fatal("indeterminate publisher failure selected another repository", err, calls)
	}
}

func TestVirtctlLatestAndExactOverrideFreezeUpstreamPublisherIdentity(t *testing.T) {
	for _, version := range []string{"latest", "v1.8.2"} {
		request := controller.ToolRequest{Kind: "virtctl", Version: version, Compatibility: "kubevirt", Mirror: "https://mirror.example.test/tools"}
		selected, complete, err := (&ToolCatalog{}).Select([]controller.ToolRequest{request}, nil)
		if err != nil || complete || len(selected) != 0 {
			t.Fatal("unresolved virtctl could not be planned", selected, complete, err)
		}
		calls := 0
		resolver := &ToolCatalog{metadata: func(_ context.Context, method, endpoint string, _ int64, _ prerequisites.SetupEgress) (toolMetadata, error) {
			calls++
			wanted := "https://api.github.com/repos/kubevirt/kubevirt/releases/latest"
			if version != "latest" {
				wanted = "https://api.github.com/repos/kubevirt/kubevirt/releases/tags/v1.8.2"
			}
			if method != http.MethodGet || endpoint != wanted {
				t.Fatal("unexpected metadata request", method, endpoint)
			}
			return toolMetadata{data: []byte(fmt.Sprintf(`{"tag_name":"v1.8.2","draft":false,"prerelease":false,"assets":[{"name":"virtctl-v1.8.2-linux-amd64","browser_download_url":"https://github.com/kubevirt/kubevirt/releases/download/v1.8.2/virtctl-v1.8.2-linux-amd64","size":12345,"digest":"sha256:%s"}]}`, strings.Repeat("e", 64)))}, nil
		}}
		resolved, err := resolver.Resolve(t.Context(), []controller.ToolRequest{request}, prerequisites.SetupEgress{})
		if err != nil || len(resolved) != 1 || calls != 1 || resolved[0].Version != "v1.8.2" || resolved[0].Archive != "binary" || resolved[0].Source.URL != "https://mirror.example.test/tools/v1.8.2/virtctl-v1.8.2-linux-amd64" {
			t.Fatal(resolved, err, calls)
		}
		selected, complete, err = (&ToolCatalog{}).Select([]controller.ToolRequest{request}, []prerequisites.DependencySource{resolved[0].Source})
		if err != nil || !complete || !reflect.DeepEqual(resolved, selected) || calls != 1 {
			t.Fatal("frozen virtctl retried mutable publisher metadata", selected, complete, err)
		}
	}
}

func TestExactPinsResolveThePublishersPrereleaseTags(t *testing.T) {
	digest := strings.Repeat("c", 64)
	for _, test := range []struct {
		request controller.ToolRequest
		url     string
		answers map[string]string
	}{
		{controller.ToolRequest{Kind: "openshift-install", Version: "4.22.0-rc.1", Compatibility: "openshift"}, "https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.22.0-rc.1/openshift-install-linux-4.22.0-rc.1.tar.gz", map[string]string{
			"https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.22.0-rc.1/sha256sum.txt": digest + "  openshift-install-linux-4.22.0-rc.1.tar.gz\n",
		}},
		{controller.ToolRequest{Kind: "virtctl", Version: "v1.9.0-rc.0", Compatibility: "kubevirt"}, "https://github.com/kubevirt/kubevirt/releases/download/v1.9.0-rc.0/virtctl-v1.9.0-rc.0-linux-amd64", map[string]string{
			"https://api.github.com/repos/kubevirt/kubevirt/releases/tags/v1.9.0-rc.0": `{"tag_name":"v1.9.0-rc.0","draft":false,"prerelease":true,"assets":[{"name":"virtctl-v1.9.0-rc.0-linux-amd64","browser_download_url":"https://github.com/kubevirt/kubevirt/releases/download/v1.9.0-rc.0/virtctl-v1.9.0-rc.0-linux-amd64","size":12345,"digest":"sha256:` + digest + `"}]}`,
		}},
		{controller.ToolRequest{Kind: "helm", Version: "4.1.0-rc.1"}, "https://get.helm.sh/helm-v4.1.0-rc.1-linux-amd64.tar.gz", map[string]string{
			"https://api.github.com/repos/helm/helm/releases/tags/v4.1.0-rc.1":  `{"tag_name":"v4.1.0-rc.1","draft":false,"prerelease":true,"assets":[]}`,
			"https://get.helm.sh/helm-v4.1.0-rc.1-linux-amd64.tar.gz.sha256sum": digest + "  helm-v4.1.0-rc.1-linux-amd64.tar.gz\n",
		}},
	} {
		t.Run(test.request.Kind, func(t *testing.T) {
			resolver := &ToolCatalog{metadata: func(_ context.Context, method, endpoint string, _ int64, _ prerequisites.SetupEgress) (toolMetadata, error) {
				if method == http.MethodHead && endpoint == test.url {
					return toolMetadata{size: 4096}, nil
				}
				answer, ok := test.answers[endpoint]
				if method != http.MethodGet || !ok {
					t.Fatal("unexpected publisher request", method, endpoint)
				}
				return toolMetadata{data: []byte(answer)}, nil
			}}
			resolved, err := resolver.Resolve(t.Context(), []controller.ToolRequest{test.request}, prerequisites.SetupEgress{})
			if err != nil || len(resolved) != 1 || resolved[0].Version != test.request.Version || resolved[0].Source.URL != test.url || resolved[0].Source.SHA256 != digest {
				t.Fatal("exact publisher tag was not resolved", resolved, err)
			}
			selected, complete, err := (&ToolCatalog{}).Select([]controller.ToolRequest{test.request}, []prerequisites.DependencySource{resolved[0].Source})
			if err != nil || !complete || !reflect.DeepEqual(selected, resolved) {
				t.Fatal("exact publisher tag was not retained", selected, complete, err)
			}
		})
	}
}

func TestVirtctlLatestRefusesUnstableOrUnprovenPublisherAssets(t *testing.T) {
	valid := `{"tag_name":"v1.8.2","draft":false,"prerelease":false,"assets":[{"name":"virtctl-v1.8.2-linux-amd64","browser_download_url":"https://github.com/kubevirt/kubevirt/releases/download/v1.8.2/virtctl-v1.8.2-linux-amd64","size":12345,"digest":"sha256:` + strings.Repeat("e", 64) + `"}]}`
	for name, payload := range map[string]string{
		"draft":           strings.Replace(valid, `"draft":false`, `"draft":true`, 1),
		"prerelease":      strings.Replace(valid, `"prerelease":false`, `"prerelease":true`, 1),
		"unstable tag":    strings.ReplaceAll(valid, "v1.8.2", "v1.8.2-rc.1"),
		"missing digest":  strings.Replace(valid, "sha256:"+strings.Repeat("e", 64), "", 1),
		"wrong publisher": strings.Replace(valid, "https://github.com/kubevirt/", "https://github.com/unapproved/", 1),
		"missing asset":   `{"tag_name":"v1.8.2","draft":false,"prerelease":false,"assets":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			resolver := &ToolCatalog{metadata: func(_ context.Context, method, endpoint string, _ int64, _ prerequisites.SetupEgress) (toolMetadata, error) {
				calls++
				if calls > 1 || method != http.MethodGet || endpoint != "https://api.github.com/repos/kubevirt/kubevirt/releases/latest" {
					t.Fatal("unproven release triggered another acquisition", method, endpoint)
				}
				return toolMetadata{data: []byte(payload)}, nil
			}}
			if _, err := resolver.Resolve(t.Context(), []controller.ToolRequest{{Kind: "virtctl", Version: "latest", Compatibility: "kubevirt"}}, prerequisites.SetupEgress{}); err == nil {
				t.Fatal("unstable or unproven release accepted")
			}
		})
	}
}
