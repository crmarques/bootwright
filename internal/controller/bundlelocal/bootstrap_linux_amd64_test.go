//go:build linux && amd64

package bundlelocal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"golang.org/x/sys/unix"
)

func TestPublisherBrokerRefusesUnapprovedContactsBeforeDial(t *testing.T) {
	broker := &publisherBroker{ctx: context.Background()}
	for _, test := range []struct{ method, authority string }{{http.MethodGet, "pypi.org:443"}, {http.MethodConnect, "example.test:443"}, {http.MethodConnect, "pypi.org:80"}, {http.MethodConnect, "pypi.org.example.test:443"}, {http.MethodConnect, "user@files.pythonhosted.org:443"}} {
		request := httptest.NewRequest(test.method, "https://pypi.org/", nil)
		request.Host = test.authority
		response := httptest.NewRecorder()
		broker.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("unapproved route contacted: %s %s", test.method, test.authority)
		}
	}
}

func TestPublisherBrokerReservesCapacityAndJoinsCanceledRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{}, 8)
	serverDone := make(chan struct{})
	close(serverDone)
	broker := &publisherBroker{ctx: ctx, cancel: cancel, done: serverDone, server: &http.Server{}, proxy: func(*http.Request) (*url.URL, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	request := func() *http.Request {
		value := httptest.NewRequest(http.MethodConnect, "https://pypi.org/", nil)
		value.Host = "pypi.org:443"
		return value
	}
	for range 8 {
		go broker.ServeHTTP(httptest.NewRecorder(), request())
	}
	for range 8 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("reserved requests did not reach their explicit route")
		}
	}
	response := httptest.NewRecorder()
	broker.ServeHTTP(response, request())
	if response.Code != http.StatusTooManyRequests {
		t.Fatal("an extra request passed the capacity bound before existing dials completed")
	}
	closed := make(chan struct{})
	go func() { broker.close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("broker shutdown did not cancel and join active requests")
	}
	if broker.active != 0 {
		t.Fatal("broker returned before its reserved requests finished")
	}
	response = httptest.NewRecorder()
	broker.ServeHTTP(response, request())
	if response.Code != http.StatusTooManyRequests {
		t.Fatal("closed broker accepted another publisher route")
	}
}

func TestBootstrapChildHasNoHostRootMapping(t *testing.T) {
	attributes := bootstrapProcessAttributes("/disposable")
	if attributes.Chroot != "/disposable" || attributes.Cloneflags&(unix.CLONE_NEWUSER|unix.CLONE_NEWNS|unix.CLONE_NEWPID) != (unix.CLONE_NEWUSER|unix.CLONE_NEWNS|unix.CLONE_NEWPID) {
		t.Fatal("resolver confinement is incomplete")
	}
	if len(attributes.UidMappings) != 1 || attributes.UidMappings[0].HostID == 0 || attributes.UidMappings[0].ContainerID != 0 || attributes.UidMappings[0].Size != 1 || attributes.Credential == nil || attributes.Credential.Uid != 0 || attributes.GidMappingsEnableSetgroups {
		t.Fatal("resolver identity can retain host-root authority")
	}
	want := os.Getuid()
	if want == 0 {
		want = 65534
	}
	if attributes.UidMappings[0].HostID != want {
		t.Fatal("resolver mapped an unrelated host identity")
	}
}

// syntheticResolver completes a resolution against fake publishers whose
// ansible-core page is of the given Index API version, counting the sources
// it fetches.
func syntheticResolver(t *testing.T, apiVersion string, fetched *int) *BootstrapCatalog {
	t.Helper()
	wheels := map[string][]byte{
		"https://files.pythonhosted.org/packages/ansible_core-2.21.4-py3-none-any.whl": wheelArchive(t, "ansible_core/__init__.py"),
		"https://files.pythonhosted.org/packages/urllib3-2.7.0-py3-none-any.whl":       wheelArchive(t, "urllib3/__init__.py"),
	}
	python := pythonArchive(t, archiveMember{name: "python/bin/python3.14", data: "interpreter"})
	resolver := NewBootstrapResolver()
	resolver.metadata = func(_ context.Context, method, endpoint string, _ prerequisites.SetupEgress) (toolMetadata, error) {
		switch {
		case method == http.MethodGet && endpoint == pythonMetadataURL:
			return toolMetadata{data: pythonMetadataFixture(t, "3.14.7")}, nil
		case method == http.MethodGet && endpoint == ansibleIndexURL:
			return toolMetadata{data: ansibleIndexFixture(t, apiVersion, indexRelease("2.21.4", false)...)}, nil
		case method == http.MethodHead:
			return toolMetadata{size: 1024}, nil
		}
		t.Fatalf("an unexpected publisher endpoint was consulted: %s %s", method, endpoint)
		return toolMetadata{}, nil
	}
	resolver.fetch = func(_ context.Context, source prerequisites.DependencySource, _ prerequisites.SetupEgress) ([]byte, error) {
		*fetched++
		if data, ok := wheels[source.URL]; ok {
			return data, nil
		}
		return python, nil
	}
	resolver.resolve = func(context.Context, *projection, prerequisites.BootstrapDefinition, prerequisites.SetupEgress) ([]byte, error) {
		install := []map[string]any{}
		for name, version := range map[string]string{"ansible-core": "2.21.4", "urllib3": "2.7.0"} {
			file := strings.ReplaceAll(name, "-", "_") + "-" + version + "-py3-none-any.whl"
			install = append(install, map[string]any{"metadata": map[string]string{"name": name, "version": version}, "download_info": map[string]any{"url": "https://files.pythonhosted.org/packages/" + file, "archive_info": map[string]any{"hashes": map[string]string{"sha256": strings.Repeat("a", 64)}}}})
		}
		return json.Marshal(map[string]any{"version": "1", "install": install, "environment": map[string]string{"python_full_version": "3.14.7", "implementation_name": "cpython", "platform_machine": "x86_64", "sys_platform": "linux"}})
	}
	return resolver
}

// A PyPI minor bump must not stop every fresh setup: a resolution over a
// newer Index API minor completes, or stops for another cause, carrying its
// warning to setup either way, while one over a newer major refuses before
// any source is fetched.
func TestAResolutionOverANewerIndexAPIMinorWarnsAndANewerMajorRefuses(t *testing.T) {
	platform := prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}
	known := "1." + strconv.Itoa(indexAPIMinor)
	for _, api := range []string{known, "1." + strconv.Itoa(indexAPIMinor+1)} {
		fetched := 0
		resolved, warnings, err := syntheticResolver(t, api, &fetched).Resolve(t.Context(), platform, controller.DefaultDependencyVersions(), prerequisites.SetupEgress{})
		if err != nil || prerequisites.ValidateBootstrap(resolved) != nil || resolved.AnsibleVersion != "2.21.4" || fetched != 3 {
			t.Fatalf("a resolution over Index API %s did not complete: %d fetched %+v", api, fetched, diagnostics.Of(err))
		}
		if api == known && len(warnings) != 0 || api != known && (len(warnings) != 1 || warnings[0].Severity != "warning" || warnings[0].Code != "controller.unsupported" || !strings.Contains(warnings[0].Message, "version "+api+", newer than the "+known+" ")) {
			t.Fatalf("a resolution over Index API %s reported %+v", api, warnings)
		}
	}
	fetched := 0
	resolved, warnings, err := syntheticResolver(t, "2.0", &fetched).Resolve(t.Context(), platform, controller.DefaultDependencyVersions(), prerequisites.SetupEgress{})
	if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "controller.unsupported" || !strings.Contains(found[0].Message, "version 2.0, a major version this build does not read") || fetched != 0 || warnings != nil || resolved.Digest != "" {
		t.Fatalf("a resolution over a newer Index API major was not refused: %d fetched %+v %+v", fetched, warnings, found)
	}
	newer, stopped := "1."+strconv.Itoa(indexAPIMinor+1), errors.New("publisher unavailable")
	for name, stop := range map[string]func(*BootstrapCatalog){
		"pip resolve": func(c *BootstrapCatalog) {
			c.resolve = func(context.Context, *projection, prerequisites.BootstrapDefinition, prerequisites.SetupEgress) ([]byte, error) {
				return nil, stopped
			}
		},
		"wheel fetch": func(c *BootstrapCatalog) {
			fetch := c.fetch
			c.fetch = func(ctx context.Context, source prerequisites.DependencySource, egress prerequisites.SetupEgress) ([]byte, error) {
				if strings.HasSuffix(source.URL, ".whl") {
					return nil, stopped
				}
				return fetch(ctx, source, egress)
			}
		},
	} {
		resolver := syntheticResolver(t, newer, &fetched)
		stop(resolver)
		resolved, warnings, err := resolver.Resolve(t.Context(), platform, controller.DefaultDependencyVersions(), prerequisites.SetupEgress{})
		if !errors.Is(err, stopped) || resolved.Digest != "" || len(warnings) != 1 || warnings[0].Severity != "warning" || !strings.Contains(warnings[0].Message, "version "+newer+", newer than the "+known+" ") {
			t.Fatalf("a resolution over Index API %s stopped by its %s lost its warning: %+v %v", newer, name, warnings, err)
		}
	}
	exact := controller.DefaultDependencyVersions()
	exact.Ansible = "2.21.5"
	fetched = 0
	resolved, warnings, err = syntheticResolver(t, newer, &fetched).Resolve(t.Context(), platform, exact, prerequisites.SetupEgress{})
	if found := diagnostics.Of(err); len(found) != 1 || !strings.Contains(found[0].Message, "no live wheel of ansible-core 2.21.5") || fetched != 0 || resolved.Digest != "" || len(warnings) != 1 || !strings.Contains(warnings[0].Message, "version "+newer+", newer than the "+known+" ") {
		t.Fatalf("a resolution over Index API %s that refused its releases lost its warning: %d fetched %+v %+v", newer, fetched, warnings, found)
	}
}
