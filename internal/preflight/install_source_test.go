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

func TestPackageSourceReachabilityProbesRepoMetadata(t *testing.T) {
	state := loadFixtureState(t, "010-ceph-3nodes-libvirt-boot-iso")
	var probed []string
	deps := Deps{HTTPDo: func(req *http.Request, insecure bool) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("probe method = %s, want GET", req.Method)
		}
		if insecure {
			t.Fatal("package-source probe must verify TLS by default")
		}
		probed = append(probed, req.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
	}}

	checks := packageSourceReachabilityChecks(state, []Phase{{Name: "machines"}}, deps, nil)
	if len(checks) != 2 {
		t.Fatalf("checks = %d, want 2 (BaseOS tree + AppStream repo, deduped across nodes): %+v", len(checks), checks)
	}
	for _, c := range checks {
		if c.Group != checkGroupPackageSource || c.Status != StatusOK {
			t.Fatalf("check = %+v, want OK in %q", c, checkGroupPackageSource)
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

func TestPackageSourceReachabilityFailsOnHTTPError(t *testing.T) {
	state := loadFixtureState(t, "010-ceph-3nodes-libvirt-boot-iso")
	deps := Deps{HTTPDo: func(*http.Request, bool) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(""))}, nil
	}}

	checks := packageSourceReachabilityChecks(state, []Phase{{Name: "machines"}}, deps, nil)
	if len(checks) != 2 {
		t.Fatalf("checks = %d, want 2", len(checks))
	}
	for _, c := range checks {
		if c.Status != StatusFail {
			t.Fatalf("check %q = %s, want FAIL on HTTP 404", c.Name, c.Status)
		}
	}
}

func TestPackageSourceReachabilityWarnsOnTransportError(t *testing.T) {
	state := loadFixtureState(t, "010-ceph-3nodes-libvirt-boot-iso")
	deps := Deps{HTTPDo: func(*http.Request, bool) (*http.Response, error) {
		return nil, errors.New("dial tcp: connection refused")
	}}

	checks := packageSourceReachabilityChecks(state, []Phase{{Name: "machines"}}, deps, nil)
	if len(checks) != 2 {
		t.Fatalf("checks = %d, want 2", len(checks))
	}
	for _, c := range checks {
		if c.Status != StatusWarn {
			t.Fatalf("check %q = %s, want WARN on transport error", c.Name, c.Status)
		}
	}
}

func TestPackageSourceReachabilitySkipsDVDMedia(t *testing.T) {
	state := loadFixtureState(t, "006-ceph-3nodes-libvirt-managed-os")
	deps := Deps{HTTPDo: func(*http.Request, bool) (*http.Response, error) {
		t.Fatal("DVD media must not be probed for a package source")
		return nil, nil
	}}

	if checks := packageSourceReachabilityChecks(state, []Phase{{Name: "machines"}}, deps, nil); len(checks) != 0 {
		t.Fatalf("checks = %+v, want none for DVD media", checks)
	}
}

func TestPackageSourceReachabilitySkipsFromSubscription(t *testing.T) {
	state := loadFixtureState(t, "010-ceph-3nodes-libvirt-boot-iso")
	state.MachineInstallProfiles[0].Spec.Installer.Anaconda.PackageSource = &v1alpha1.MachineInstallPackageSource{
		FromSubscription: &v1alpha1.MachineInstallPackageFromSubscription{
			EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
		},
	}
	deps := Deps{HTTPDo: func(*http.Request, bool) (*http.Response, error) {
		t.Fatal("fromSubscription package sources must not be probed")
		return nil, nil
	}}

	if checks := packageSourceReachabilityChecks(state, []Phase{{Name: "machines"}}, deps, nil); len(checks) != 0 {
		t.Fatalf("checks = %+v, want none for fromSubscription", checks)
	}
}

func TestPackageSourceReachabilityYumVariableReportsInfo(t *testing.T) {
	state := loadFixtureState(t, "010-ceph-3nodes-libvirt-boot-iso")
	state.MachineInstallProfiles[0].Spec.Installer.Anaconda.PackageSource = &v1alpha1.MachineInstallPackageSource{
		Mirror: &v1alpha1.MachineInstallPackageMirror{
			BaseURL: "https://mirror.example.test/rhel/9/BaseOS/$basearch/os/",
		},
	}
	deps := Deps{HTTPDo: func(*http.Request, bool) (*http.Response, error) {
		t.Fatal("a yum-variable baseURL must not be probed")
		return nil, nil
	}}

	checks := packageSourceReachabilityChecks(state, []Phase{{Name: "machines"}}, deps, nil)
	if len(checks) != 1 || checks[0].Status != StatusInfo {
		t.Fatalf("checks = %+v, want one INFO for a yum-variable baseURL", checks)
	}
}

func TestPackageSourceReachabilitySkippedOutsideMachinesPhase(t *testing.T) {
	state := loadFixtureState(t, "010-ceph-3nodes-libvirt-boot-iso")
	deps := Deps{HTTPDo: func(*http.Request, bool) (*http.Response, error) {
		t.Fatal("probe must not run outside the machines phase")
		return nil, nil
	}}

	if checks := packageSourceReachabilityChecks(state, []Phase{{Name: "add-ons"}}, deps, nil); len(checks) != 0 {
		t.Fatalf("checks = %+v, want none outside the machines phase", checks)
	}
}

func TestCollectChecksIncludesPackageSource(t *testing.T) {
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
	checks := CollectChecks(state, []Phase{{Name: "machines"}}, true, "test", "/context/secrets", "/host-state", "/runs", deps, nil, nil)
	assertPreflightCheckStatus(t, checks, "https://mirror.example.test/rhel/9/BaseOS/x86_64/os/", "OK")
}
