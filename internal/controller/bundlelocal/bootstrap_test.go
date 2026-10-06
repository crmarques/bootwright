package bundlelocal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func pythonMetadataFixture(t *testing.T, versions ...string) []byte {
	t.Helper()
	records := map[string]any{}
	for _, version := range versions {
		var major, minor, patch int
		parts := strings.Split(version, ".")
		for index, target := range []*int{&major, &minor, &patch} {
			for _, c := range parts[index] {
				*target = *target*10 + int(c-'0')
			}
		}
		records[version] = map[string]any{"name": "cpython", "os": "linux", "libc": "gnu", "arch": map[string]any{"family": "x86_64", "variant": nil}, "major": major, "minor": minor, "patch": patch, "prerelease": "", "variant": nil, "build": "20260901", "sha256": strings.Repeat("a", 64), "url": "https://github.com/astral-sh/python-build-standalone/releases/download/20260901/cpython-" + version + "%2B20260901-x86_64-unknown-linux-gnu-install_only_stripped.tar.gz"}
	}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPythonLatestIsTheNewestQualifiedMinor(t *testing.T) {
	data := pythonMetadataFixture(t, "3.12.14", "3.13.15", "3.14.7")
	for request, want := range map[string]string{"latest": "3.14.7", "3.13.15": "3.13.15"} {
		got, source, err := selectPythonArtifact(data, request)
		if err != nil || got != want || source.SHA256 != strings.Repeat("a", 64) {
			t.Fatalf("%s: %q %#v %v", request, got, source, err)
		}
	}
	if _, _, err := selectPythonArtifact(data, "3.13.14"); err == nil {
		t.Fatal("unpublished exact version accepted")
	}
	newer := pythonMetadataFixture(t, "3.14.7", "3.14.8")
	if got, _, err := selectPythonArtifact(newer, "latest"); err != nil || got != "3.14.8" {
		t.Fatalf("new publisher release not selected: %s %v", got, err)
	}
	// A newer minor than the qualified ansible-core supports as a controller
	// is neither selected nor a reason to refuse.
	unqualified := pythonMetadataFixture(t, "3.14.7", "3.15.0", "3.16.1")
	if got, _, err := selectPythonArtifact(unqualified, "latest"); err != nil || got != "3.14.7" {
		t.Fatalf("latest left the qualified controller minors: %s %v", got, err)
	}
	if got, _, err := selectPythonArtifact(unqualified, "3.15.0"); err == nil {
		t.Fatalf("an exact unqualified controller Python was selected: %s", got)
	}
}

// ansibleFile is one file of a PyPI release listing, shaped like the JSON
// API's per-file record.
func ansibleFile(version, packageType, requiresPython string, yanked bool) map[string]any {
	filename := "ansible_core-" + version + ".tar.gz"
	if packageType == "bdist_wheel" {
		filename = "ansible_core-" + version + "-py3-none-any.whl"
	}
	return map[string]any{"filename": filename, "packagetype": packageType, "python_version": "py3", "requires_python": requiresPython, "yanked": yanked, "yanked_reason": nil, "url": "https://files.pythonhosted.org/packages/" + filename, "digests": map[string]string{"sha256": strings.Repeat("a", 64)}}
}

// indexEntry is one file of PyPI's Index API project page in its PEP 691
// JSON form, keyed like the api-version 1.4 example at
// https://docs.pypi.org/api/index-api/; the selector reads only its filename
// and yank. The live page (api-version 1.4, read 2026-09-29) yanks
// 2.14.0rc1.post0 with the reason "incorrect build".
func indexEntry(version, kind string, yanked any) map[string]any {
	filename := "ansible_core-" + version + ".tar.gz"
	if kind == "wheel" {
		filename = "ansible_core-" + version + "-py3-none-any.whl"
	}
	return map[string]any{"core-metadata": false, "data-dist-info-metadata": false, "filename": filename, "hashes": map[string]string{"sha256": strings.Repeat("a", 64)}, "requires-python": ">=3.12", "size": 1024, "upload-time": "2026-09-01T00:00:00.000000Z", "url": "https://files.pythonhosted.org/packages/" + filename, "yanked": yanked}
}

// ansibleIndexFixture wraps files in the project page, listing their
// versions as PEP 700's versions key does.
func ansibleIndexFixture(t *testing.T, apiVersion string, files ...map[string]any) []byte {
	t.Helper()
	versions := []string{}
	for _, file := range files {
		version := strings.TrimPrefix(file["filename"].(string), "ansible_core-")
		version = strings.TrimSuffix(strings.TrimSuffix(version, ".tar.gz"), "-py3-none-any.whl")
		if !slices.Contains(versions, version) {
			versions = append(versions, version)
		}
	}
	data, err := json.Marshal(map[string]any{"files": files, "meta": map[string]any{"_last-serial": 30000000, "api-version": apiVersion}, "name": "ansible-core", "project-status": map[string]string{"status": "active"}, "versions": versions})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func indexRelease(version string, yanked any) []map[string]any {
	return []map[string]any{indexEntry(version, "wheel", yanked), indexEntry(version, "sdist", yanked)}
}

func TestAnsibleLatestIsTheNewestQualifiedPatch(t *testing.T) {
	files := []map[string]any{indexEntry("2.21.13", "sdist", false)}
	for _, version := range []string{"2.20.9", "2.21.4", "2.21.9", "2.21.10", "2.21.12rc1", "2.22.0b1", "2.22.0"} {
		files = append(files, indexRelease(version, false)...)
	}
	files = append(files, indexRelease("2.21.11", true)...)
	files = append(files, indexRelease("2.21.14", "incorrect build")...)
	for _, api := range []string{"1.0", "1.4"} {
		if got, _, err := selectAnsibleRelease(ansibleIndexFixture(t, api, files...), "latest", "3.14.7"); err != nil || got != "2.21.10" {
			t.Fatalf("api-version %s: latest is not the newest qualified patch with a live wheel: %s %+v", api, got, diagnostics.Of(err))
		}
	}
	unmarked := indexEntry("2.21.15", "wheel", nil)
	delete(unmarked, "yanked")
	if got, _, err := selectAnsibleRelease(ansibleIndexFixture(t, "1.4", append(files, unmarked)...), "latest", "3.14.7"); err != nil || got != "2.21.15" {
		t.Fatalf("a wheel without a yanked key is not live: %s %+v", got, diagnostics.Of(err))
	}
	none := append(indexRelease("2.20.9", false), indexEntry("2.21.13", "sdist", false), indexEntry("2.21.14", "wheel", true))
	got, _, err := selectAnsibleRelease(ansibleIndexFixture(t, "1.4", append(none, indexRelease("2.22.0", false)...)...), "latest", "3.14.7")
	if found := diagnostics.Of(err); len(found) != 1 || !strings.Contains(found[0].Message, "ansible-core "+prerequisites.QualifiedAnsibleMinor+" ") {
		t.Fatalf("latest did not fail closed on the qualified minor: %s %+v", got, found)
	}
}

// A page without a well-formed PEP 629 API version is not an Index API page,
// and PEP 691 allows a yank only as a Boolean or a non-empty reason.
func TestAnsibleLatestRefusesAnIndexPageOutsidePEP691(t *testing.T) {
	valid := indexEntry("2.21.4", "wheel", false)
	page := func(api string, files ...map[string]any) []byte {
		return ansibleIndexFixture(t, api, append([]map[string]any{valid}, files...)...)
	}
	projectJSON, err := json.Marshal(map[string]any{"info": map[string]any{"name": "ansible-core", "version": "2.21.4"}, "releases": map[string]any{"2.21.4": []map[string]any{ansibleFile("2.21.4", "bdist_wheel", ">=3.12", false)}}})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"major 0":           page("0.4"),
		"no api-version":    page(""),
		"major only":        page("1"),
		"unbounded minor":   page("1.99999999999999999999"),
		"foreign project":   []byte(strings.Replace(string(page("1.4")), `"name":"ansible-core"`, `"name":"ansible"`, 1)),
		"null yank":         page("1.4", indexEntry("2.21.3", "sdist", nil)),
		"empty yank reason": page("1.4", indexEntry("2.21.3", "sdist", "")),
		"numeric yank":      page("1.4", indexEntry("2.21.3", "sdist", 0)),
		"project JSON":      projectJSON,
		"HTML page":         []byte(`<!DOCTYPE html><html><body><a href="https://files.pythonhosted.org/packages/ansible_core-2.21.4-py3-none-any.whl">ansible_core-2.21.4-py3-none-any.whl</a></body></html>`),
	} {
		got, _, err := selectAnsibleRelease(data, "latest", "3.14.7")
		if found := diagnostics.Of(err); len(found) != 1 || !strings.Contains(found[0].Message, "no valid publisher metadata") {
			t.Fatalf("%s: an index page outside PEP 691 was read: %s %+v", name, got, found)
		}
	}
	if got, _, err := selectAnsibleRelease(page("1.4"), "latest", "3.14.7"); err != nil || got != "2.21.4" {
		t.Fatalf("the unmutated page was refused: %s %+v", got, diagnostics.Of(err))
	}
}

func TestAnsibleIndexIsNegotiatedAsPEP691JSON(t *testing.T) {
	page := ansibleIndexFixture(t, "1.4", indexEntry("2.21.4", "wheel", false))
	for _, test := range []struct {
		name, contentType string
		pass              bool
	}{
		{"json", simpleIndexJSON, true},
		{"html", "application/vnd.pypi.simple.v1+html", false},
		{"legacy html", "text/html", false},
		{"untyped", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			metadata, err := receiveBootstrapMetadata(t.Context(), http.MethodGet, ansibleIndexURL, func(request *http.Request) (*http.Response, error) {
				if request.URL.String() != ansibleIndexURL || request.Header.Get("Accept") != simpleIndexJSON || request.Header.Get("Accept-Encoding") != "identity" {
					t.Fatalf("index request did not ask for the PEP 691 JSON form: %v", request.Header)
				}
				return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(page)), Header: http.Header{"Content-Type": {test.contentType}}, Body: io.NopCloser(bytes.NewReader(page))}, nil
			})
			if (err == nil) != test.pass || test.pass && !bytes.Equal(metadata.data, page) {
				t.Fatalf("Content-Type %q: unexpected result %+v", test.contentType, diagnostics.Of(err))
			}
		})
	}
	python := pythonMetadataFixture(t, "3.14.7")
	metadata, err := receiveBootstrapMetadata(t.Context(), http.MethodGet, pythonMetadataURL, func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Accept") != "" {
			t.Fatal("a publisher outside the Index API was negotiated")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/plain; charset=utf-8"}}, Body: io.NopCloser(bytes.NewReader(python))}, nil
	})
	if err != nil || !bytes.Equal(metadata.data, python) {
		t.Fatalf("Python publisher metadata was refused: %+v", diagnostics.Of(err))
	}
}

func TestAnsibleIsSelectedFromTheIndexAPI(t *testing.T) {
	for _, intent := range []string{"latest", "2.21.4"} {
		resolver := NewBootstrapResolver(nil)
		requested := []string{}
		resolver.metadata = func(_ context.Context, method, endpoint string, _ prerequisites.SetupEgress) (toolMetadata, error) {
			requested = append(requested, method+" "+endpoint)
			switch {
			case method == http.MethodGet && endpoint == pythonMetadataURL:
				return toolMetadata{data: pythonMetadataFixture(t, "3.14.7")}, nil
			case method == http.MethodHead:
				return toolMetadata{size: 1024}, nil
			case method == http.MethodGet && endpoint == ansibleIndexURL:
				return toolMetadata{data: ansibleIndexFixture(t, "1.4", indexRelease("2.21.4", false)...)}, nil
			}
			t.Fatalf("%s ansible-core consulted another publisher endpoint: %s %s", intent, method, endpoint)
			return toolMetadata{}, nil
		}
		selected := errors.New("selection finished")
		resolver.fetch = func(context.Context, prerequisites.DependencySource, prerequisites.SetupEgress) ([]byte, error) {
			return nil, selected
		}
		versions := controller.DefaultDependencyVersions()
		versions.Ansible = intent
		_, _, err := resolver.Resolve(t.Context(), prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}, versions, prerequisites.SetupEgress{})
		if !errors.Is(err, selected) || !slices.Contains(requested, http.MethodGet+" "+ansibleIndexURL) {
			t.Fatalf("%s was not selected from the Index API: %v %+v %v", intent, requested, diagnostics.Of(err), err)
		}
	}
}

func TestAnsibleDoesNotDowngradeForIncompatiblePython(t *testing.T) {
	// pip's exact-root resolve enforces Requires-Python; the selector never
	// yields to an older release that the selected interpreter could run.
	newer := indexEntry("2.21.5", "wheel", false)
	newer["requires-python"] = ">=3.15"
	project := ansibleIndexFixture(t, "1.4", indexEntry("2.21.4", "wheel", false), newer)
	if version, _, err := selectAnsibleRelease(project, "latest", "3.14.7"); err != nil || version != "2.21.5" {
		t.Fatalf("latest yielded to an older release for the selected Python: %s %v", version, err)
	}
	if version, _, err := selectAnsibleRelease(project, "latest", "3.15.0"); err == nil {
		t.Fatalf("an unqualified controller Python was accepted: %s", version)
	}
}

// An exact intent reads the same Index API page latest does and takes its
// release only when that release is a candidate latest could select.
func TestAnExactAnsibleIsOneOfTheIndexPagesCandidates(t *testing.T) {
	files := append(indexRelease("2.21.4", false), indexRelease("2.21.8", false)...)
	files = append(files, indexRelease("2.20.9", false)...)
	files = append(files, indexRelease("2.21.5", true)...)
	files = append(files, indexRelease("2.21.6", "incorrect build")...)
	files = append(files, indexEntry("2.21.7", "sdist", false))
	page := ansibleIndexFixture(t, "1.4", files...)
	if version, _, err := selectAnsibleRelease(page, "2.21.4", "3.14.7"); err != nil || version != "2.21.4" {
		t.Fatalf("an exact live release was not selected: %s %+v", version, diagnostics.Of(err))
	}
	for requested, reason := range map[string]string{
		"2.21.5": "yanked", "2.21.6": "yanked with a reason", "2.21.7": "without a wheel", "2.21.3": "unlisted",
	} {
		version, _, err := selectAnsibleRelease(page, requested, "3.14.7")
		if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "controller.setup" || !strings.Contains(found[0].Message, "no live wheel of ansible-core "+requested) {
			t.Fatalf("an exact release %s was selected: %s %+v", reason, version, found)
		}
	}
	version, _, err := selectAnsibleRelease(page, "2.20.9", "3.14.7")
	if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "controller.unsupported" || !strings.Contains(found[0].Message, "ansible-core "+prerequisites.QualifiedAnsibleMinor+" ") {
		t.Fatalf("an exact release outside the qualified minor was selected: %s %+v", version, found)
	}
	projectJSON, err := json.Marshal(map[string]any{"info": map[string]any{"name": "ansible-core", "version": "2.21.4"}, "urls": []map[string]any{ansibleFile("2.21.4", "bdist_wheel", ">=3.12", false)}})
	if err != nil {
		t.Fatal(err)
	}
	version, _, err = selectAnsibleRelease(projectJSON, "2.21.4", "3.14.7")
	if found := diagnostics.Of(err); len(found) != 1 || !strings.Contains(found[0].Message, "no valid publisher metadata") {
		t.Fatalf("an exact release was read from the version-specific project JSON: %s %+v", version, found)
	}
}

// PEP 629 has a client warn of an Index API minor newer than it knows and
// refuse a newer major, for latest and an exact intent alike.
func TestAnIndexPageOfANewerMinorWarnsAndANewerMajorRefuses(t *testing.T) {
	files := indexRelease("2.21.4", false)
	known := "1." + strconv.Itoa(indexAPIMinor)
	for _, requested := range []string{"latest", "2.21.4"} {
		if version, warnings, err := selectAnsibleRelease(ansibleIndexFixture(t, known, files...), requested, "3.14.7"); err != nil || version != "2.21.4" || len(warnings) != 0 {
			t.Fatalf("%s: the newest minor this build reads was not read plainly: %s %+v %+v", requested, version, warnings, diagnostics.Of(err))
		}
		for _, api := range []string{"1." + strconv.Itoa(indexAPIMinor+1), "1.10"} {
			version, warnings, err := selectAnsibleRelease(ansibleIndexFixture(t, api, files...), requested, "3.14.7")
			if err != nil || version != "2.21.4" || len(warnings) != 1 || warnings[0].Severity != "warning" || warnings[0].Code != "controller.unsupported" || !strings.Contains(warnings[0].Message, "version "+api+", newer than the "+known+" ") || warnings[0].Remediation == "" {
				t.Fatalf("%s: an Index API page of version %s was not read with a warning: %s %+v %+v", requested, api, version, warnings, diagnostics.Of(err))
			}
			for _, refused := range [][]map[string]any{indexRelease("2.21.4", true), indexRelease("2.21.4", 42)} {
				version, warnings, err := selectAnsibleRelease(ansibleIndexFixture(t, api, refused...), requested, "3.14.7")
				if err == nil || version != "" || len(warnings) != 1 || !strings.Contains(warnings[0].Message, "version "+api+", newer than the "+known+" ") {
					t.Fatalf("%s: an Index API page of version %s whose releases were refused lost its warning: %s %+v %+v", requested, api, version, warnings, diagnostics.Of(err))
				}
			}
		}
		for _, api := range []string{"2.0", "2." + strconv.Itoa(indexAPIMinor), "10.0"} {
			version, warnings, err := selectAnsibleRelease(ansibleIndexFixture(t, api, files...), requested, "3.14.7")
			if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "controller.unsupported" || !strings.Contains(found[0].Message, "version "+api+", a major version this build does not read") || found[0].Remediation == "" || version != "" || warnings != nil {
				t.Fatalf("%s: an Index API page of version %s was read: %s %+v %+v", requested, api, version, warnings, found)
			}
		}
	}
}

func TestPythonPublisherRejectsForeignArtifact(t *testing.T) {
	data := pythonMetadataFixture(t, "3.14.7")
	data = []byte(strings.ReplaceAll(string(data), "github.com/astral-sh", "example.test/astral-sh"))
	if _, _, err := selectPythonArtifact(data, "latest"); err == nil {
		t.Fatal("foreign Python artifact accepted")
	}
}

func TestExactReleasesOutsideTheQualifiedSetRefuseBeforeResolverEffects(t *testing.T) {
	for _, exact := range []struct{ python, ansible, message string }{
		{"latest", "2.18.99", "ansible-core " + prerequisites.QualifiedAnsibleMinor + " "},
		{"latest", "2.20.9", "ansible-core " + prerequisites.QualifiedAnsibleMinor + " "},
		{"latest", "2.22.0", "ansible-core " + prerequisites.QualifiedAnsibleMinor + " "},
		{"3.15.0", "latest", "CPython " + strings.Join(prerequisites.QualifiedControllerPythons(), ", ") + " "},
	} {
		resolver := NewBootstrapResolver(nil)
		resolver.metadata = func(context.Context, string, string, prerequisites.SetupEgress) (toolMetadata, error) {
			t.Fatalf("exact Python %s, Ansible %s reached publisher metadata", exact.python, exact.ansible)
			return toolMetadata{}, nil
		}
		versions := controller.DefaultDependencyVersions()
		versions.Python, versions.Ansible = exact.python, exact.ansible
		_, _, err := resolver.Resolve(context.Background(), prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}, versions, prerequisites.SetupEgress{})
		found := diagnostics.Of(err)
		if len(found) != 1 || found[0].Code != "controller.unsupported" || !strings.Contains(found[0].Message, exact.message) {
			t.Fatalf("exact Python %s, Ansible %s lacks the qualified-set diagnostic: %+v", exact.python, exact.ansible, found)
		}
	}
}

func TestPipReportRejectsUnapprovedResolutionEffects(t *testing.T) {
	item := func(name, version string) map[string]any {
		return map[string]any{"metadata": map[string]string{"name": name, "version": version}, "download_info": map[string]any{"url": "https://files.pythonhosted.org/packages/" + name + ".whl", "archive_info": map[string]any{"hashes": map[string]string{"sha256": strings.Repeat("a", 64)}}}}
	}
	for _, mutation := range []string{"valid", "direct", "yanked", "foreign", "sdist", "missing-hash", "wrong-root", "unknown-report", "duplicate", "foreign-python"} {
		t.Run(mutation, func(t *testing.T) {
			items := []map[string]any{item("ansible-core", "2.21.4"), item("urllib3", "2.7.0")}
			report := map[string]any{"version": "1", "install": items, "environment": map[string]string{"python_full_version": "3.14.7", "implementation_name": "cpython", "platform_machine": "x86_64", "sys_platform": "linux"}}
			switch mutation {
			case "direct":
				items[0]["is_direct"] = true
			case "yanked":
				items[0]["is_yanked"] = true
			case "foreign":
				items[0]["download_info"].(map[string]any)["url"] = "https://example.test/source.whl"
			case "sdist":
				items[0]["download_info"].(map[string]any)["url"] = "https://files.pythonhosted.org/packages/source.tar.gz"
			case "missing-hash":
				delete(items[0]["download_info"].(map[string]any)["archive_info"].(map[string]any)["hashes"].(map[string]string), "sha256")
			case "wrong-root":
				items[0]["metadata"].(map[string]string)["version"] = "2.20.1"
			case "unknown-report":
				report["version"] = "2"
			case "foreign-python":
				report["environment"].(map[string]string)["python_full_version"] = "3.13.15"
			case "duplicate":
				report["install"] = append(items, item("Ansible_Core", "2.21.4"))
			}
			data, _ := json.Marshal(report)
			wheels, sources, err := parsePipReport(data, prerequisites.BootstrapDefinition{AnsibleVersion: "2.21.4", PythonVersion: "3.14.7"})
			if mutation == "valid" {
				if err != nil || len(wheels) != 2 || len(sources) != 2 {
					t.Fatalf("valid report rejected: %v", err)
				}
			} else if err == nil {
				t.Fatal("unapproved resolver result accepted")
			}
		})
	}
}

func TestProjectionIdentityIncludesFileModeAndSelectedPythonLayout(t *testing.T) {
	p := newProjection()
	p.site = "python/lib/python3.14/site-packages/"
	if err := p.add("python/bin/python3.14", []byte("executable"), true); err != nil {
		t.Fatal(err)
	}
	before := p.identity()
	p.files["python/bin/python3.14"] = projectedFile{data: []byte("executable")}
	if p.identity() == before {
		t.Fatal("projection digest omitted executable mode")
	}
}
