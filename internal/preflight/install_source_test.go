package preflight

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// The 010 fixture's three ceph nodes install from the same boot image, whose
// url install source declares a BaseOS install tree plus an AppStream repo. The
// check probes each unique base URL's repodata/repomd.xml once (deduped across
// nodes) and verifies TLS by default.
func TestInstallSourceReachabilityProbesRepoMetadata(t *testing.T) {
	state := loadFixtureState(t, "010-ceph-3nodes-libvirt-boot-iso")
	var probed []string
	deps := Deps{HTTPDo: func(req *http.Request, insecure bool) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("probe method = %s, want GET", req.Method)
		}
		if insecure {
			t.Fatal("install-source probe must verify TLS by default")
		}
		probed = append(probed, req.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
	}}

	checks := installSourceReachabilityChecks(state, []Phase{{Name: "machines"}}, deps, nil)
	if len(checks) != 2 {
		t.Fatalf("checks = %d, want 2 (BaseOS tree + AppStream repo, deduped across nodes): %+v", len(checks), checks)
	}
	for _, c := range checks {
		if c.Group != checkGroupInstallSource || c.Status != StatusOK {
			t.Fatalf("check = %+v, want OK in %q", c, checkGroupInstallSource)
		}
	}
	if len(probed) != 2 {
		t.Fatalf("probed %d URLs, want 2", len(probed))
	}
	for _, u := range probed {
		if !strings.HasSuffix(u, "/repodata/repomd.xml") {
			t.Fatalf("probed %q, want a repodata/repomd.xml URL", u)
		}
	}
}

// A server that answers but serves no yum metadata at the path means a wrong
// install-tree URL — a definitive failure, not an ambiguous reachability blip.
func TestInstallSourceReachabilityFailsOnHTTPError(t *testing.T) {
	state := loadFixtureState(t, "010-ceph-3nodes-libvirt-boot-iso")
	deps := Deps{HTTPDo: func(*http.Request, bool) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(""))}, nil
	}}

	checks := installSourceReachabilityChecks(state, []Phase{{Name: "machines"}}, deps, nil)
	if len(checks) != 2 {
		t.Fatalf("checks = %d, want 2", len(checks))
	}
	for _, c := range checks {
		if c.Status != StatusFail {
			t.Fatalf("check %q = %s, want FAIL on HTTP 404", c.Name, c.Status)
		}
	}
}

// A controller that cannot reach the mirror may simply not share the install
// network, so a transport error only warns — the install target is the
// authoritative fetcher.
func TestInstallSourceReachabilityWarnsOnTransportError(t *testing.T) {
	state := loadFixtureState(t, "010-ceph-3nodes-libvirt-boot-iso")
	deps := Deps{HTTPDo: func(*http.Request, bool) (*http.Response, error) {
		return nil, errors.New("dial tcp: connection refused")
	}}

	checks := installSourceReachabilityChecks(state, []Phase{{Name: "machines"}}, deps, nil)
	if len(checks) != 2 {
		t.Fatalf("checks = %d, want 2", len(checks))
	}
	for _, c := range checks {
		if c.Status != StatusWarn {
			t.Fatalf("check %q = %s, want WARN on transport error", c.Name, c.Status)
		}
	}
}

// DVD media carries its own packages, so it has no install source to probe.
func TestInstallSourceReachabilitySkipsDVDMedia(t *testing.T) {
	state := loadFixtureState(t, "006-ceph-3nodes-libvirt-managed-os")
	deps := Deps{HTTPDo: func(*http.Request, bool) (*http.Response, error) {
		t.Fatal("DVD media must not be probed for an install source")
		return nil, nil
	}}

	if checks := installSourceReachabilityChecks(state, []Phase{{Name: "machines"}}, deps, nil); len(checks) != 0 {
		t.Fatalf("checks = %+v, want none for DVD media", checks)
	}
}

// redhatCDN reachability hides behind entitlement auth and is covered by the
// entitlement secret-material checks, so it is not probed here.
func TestInstallSourceReachabilitySkipsRedhatCDN(t *testing.T) {
	state := loadFixtureState(t, "010-ceph-3nodes-libvirt-boot-iso")
	state.MachineImages[0].Spec.InstallSource = v1alpha1.MachineImageInstallSource{
		Type:           v1alpha1.MachineImageInstallSourceTypeRHSM,
		EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
	}
	deps := Deps{HTTPDo: func(*http.Request, bool) (*http.Response, error) {
		t.Fatal("redhatCDN install sources must not be probed")
		return nil, nil
	}}

	if checks := installSourceReachabilityChecks(state, []Phase{{Name: "machines"}}, deps, nil); len(checks) != 0 {
		t.Fatalf("checks = %+v, want none for redhatCDN", checks)
	}
}

// The install source is only needed once the machines phase provisions the OS.
func TestInstallSourceReachabilitySkippedOutsideMachinesPhase(t *testing.T) {
	state := loadFixtureState(t, "010-ceph-3nodes-libvirt-boot-iso")
	deps := Deps{HTTPDo: func(*http.Request, bool) (*http.Response, error) {
		t.Fatal("probe must not run outside the machines phase")
		return nil, nil
	}}

	if checks := installSourceReachabilityChecks(state, []Phase{{Name: "addons"}}, deps, nil); len(checks) != 0 {
		t.Fatalf("checks = %+v, want none outside the machines phase", checks)
	}
}

// CollectChecks must wire the install-source probe into the apply/preflight
// host check alongside the installer-media check.
func TestCollectChecksIncludesInstallSource(t *testing.T) {
	state := loadFixtureState(t, "010-ceph-3nodes-libvirt-boot-iso")
	deps := Deps{
		LookPath:      func(name string, _ []string) (string, error) { return "/bin/" + name, nil },
		StatPath:      func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		CommandOutput: func(string, ...string) ([]byte, error) { return []byte("Python 3.12.4"), nil },
		UID:           func() int { return 0 },
		HTTPDo: func(*http.Request, bool) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
		},
	}
	checks := CollectChecks(state, []Phase{{Name: "machines"}}, true, "test", "/context/secrets", "/host-state", deps, nil, nil)
	assertPreflightCheckStatus(t, checks, "https://mirror.example.test/rhel/9/BaseOS/x86_64/os/", "OK")
}
