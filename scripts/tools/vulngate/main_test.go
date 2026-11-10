package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fixture struct {
	dir     string
	options options
}

const (
	namedModules  = `{"config":{"scan_mode":"binary"}}` + "\n" + `{"SBOM":{"go_version":"go1.26.10","modules":[{"path":"example.com/client","version":"v1.0.0"},{"path":"golang.org/x/net","version":"v0.57.0"}]}}` + "\n"
	stdlibOnly    = `{"config":{"scan_mode":"binary"}}` + "\n" + `{"SBOM":{"go_version":"go1.26.4","modules":[{"path":"stdlib","version":"go1.26.4"}]}}` + "\n"
	symbolFinding = `{"finding":{"osv":"GO-2026-6603","fixed_version":"v0.60.0","trace":[{"module":"golang.org/x/net","version":"v0.57.0","package":"golang.org/x/net/http2","function":"Close"}]}}` + "\n"
	moduleOnly    = `{"finding":{"osv":"GO-2025-3521","trace":[{"module":"k8s.io/kubernetes","version":"v1.37.2"}]}}` + "\n"
)

func newFixture(t *testing.T, vulns map[string][]string, govulncheck string, entries []exception) fixture {
	return newClientFixture(t, vulns, govulncheck, entries, map[string]string{"helm": namedModules, "virtctl": stdlibOnly})
}

func newClientFixture(t *testing.T, vulns map[string][]string, govulncheck string, entries []exception, clients map[string]string) fixture {
	t.Helper()
	dir := t.TempDir()
	write := func(name, data string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Queries []osvQuery `json:"queries"`
		}
		if r.URL.Path != "/v1/querybatch" || json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		type result struct {
			Vulns []map[string]string `json:"vulns,omitempty"`
		}
		answer := struct {
			Results []result `json:"results"`
		}{}
		for _, query := range request.Queries {
			found := result{}
			for _, id := range vulns[query.Package.Name+"@"+query.Version] {
				found.Vulns = append(found.Vulns, map[string]string{"id": id})
			}
			answer.Results = append(answer.Results, found)
		}
		_ = json.NewEncoder(w).Encode(answer)
	}))
	t.Cleanup(server.Close)
	data, err := json.Marshal(exceptions{Format: exceptionsFormat, Exceptions: entries})
	if err != nil {
		t.Fatal(err)
	}
	return fixture{dir: dir, options: options{
		govulncheck: write("govulncheck.json", govulncheck),
		lock:        write("lock.json", `{"installer":{"name":"pip","version":"26.2.1","filename":"pip.whl"},"abis":{"3.12":[{"name":"jinja2","version":"3.1.6","filename":"jinja2.whl"}],"3.14":[{"name":"jinja2","version":"3.1.6","filename":"jinja2.whl"}]}}`),
		table:       write("table.json", `{"tools":[{"kind":"helm","default":{"version":"4.3.1"}},{"kind":"virtctl","default":{"version":"1.9.1"}}]}`),
		exceptions:  write("exceptions.json", string(data)),
		verdict:     filepath.Join(dir, "verdict"),
		osv:         server.URL,
		today:       time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC),
		client:      server.Client(),
		scanClient: func(entry pin) ([]byte, error) {
			data, found := clients[entry.Kind]
			if !found {
				return nil, errors.New("no scan of " + entry.Kind)
			}
			return []byte(data), nil
		},
	}}
}

const scanned = `{"config":{"db_last_modified":"2026-11-05T23:49:57Z"}}
{"finding":{"osv":"GO-2026-1111","trace":[{"module":"golang.org/x/crypto","version":"v0.58.0"}]}}
`

func excepted(id, component, expires string) exception {
	return exception{ID: id, Component: component, State: "not_affected", Justification: "code_not_present", Reason: "the affected code runs elsewhere", Expires: expires}
}

func TestAModuleLevelGoFindingAndACleanTablePass(t *testing.T) {
	f := newFixture(t, nil, scanned, nil)
	var out strings.Builder
	if err := run(f.options, &out); err != nil {
		t.Fatalf("a clean build failed: %v\n%s", err, out.String())
	}
	if data, _ := os.ReadFile(f.options.verdict); string(data) != "passed:2026-11-10\n" {
		t.Fatalf("verdict = %q", data)
	}
}

func TestAReachableGoFindingNeedsAnException(t *testing.T) {
	reached := scanned + `{"finding":{"osv":"GO-2026-2222","trace":[{"module":"stdlib","version":"v1.26.10","function":"Dial"}]}}` + "\n"
	f := newFixture(t, nil, reached, nil)
	var out strings.Builder
	if err := run(f.options, &out); err == nil || !strings.Contains(out.String(), "GO-2026-2222 in go:stdlib@1.26.10 has no exception") {
		t.Fatalf("a reachable finding passed: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(f.options.verdict); err == nil {
		t.Fatal("a failing gate wrote a verdict")
	}
	covered := newFixture(t, nil, reached, []exception{excepted("GO-2026-2222", "go:stdlib@1.26.10", "2027-01-01")})
	if err := run(covered.options, &out); err != nil {
		t.Fatalf("a covered finding failed: %v", err)
	}
}

func TestAPinnedFindingIsCoveredByItsIDOrAnAliasOnItsComponent(t *testing.T) {
	vulns := map[string][]string{"kubevirt.io/kubevirt@1.9.1": {"GO-2026-6914", "GHSA-q3jc-42cq-33fj"}, "jinja2@3.1.6": {"PYSEC-2026-1"}}
	entry := excepted("GHSA-q3jc-42cq-33fj", "tool:virtctl@1.9.1", "2027-02-08")
	entry.Aliases = []string{"GO-2026-6914"}
	f := newFixture(t, vulns, scanned, []exception{entry, excepted("PYSEC-2026-1", "pypi:jinja2.whl", "2027-02-08")})
	var out strings.Builder
	if err := run(f.options, &out); err != nil {
		t.Fatalf("covered findings failed: %v\n%s", err, out.String())
	}
	wrong := newFixture(t, vulns, scanned, []exception{entry, excepted("PYSEC-2026-1", "pypi:pip.whl", "2027-02-08")})
	out.Reset()
	if err := run(wrong.options, &out); err == nil || !strings.Contains(out.String(), "PYSEC-2026-1 in pypi:jinja2.whl has no exception") || !strings.Contains(out.String(), "matches no finding") {
		t.Fatalf("an exception on another component covered a finding: %v\n%s", err, out.String())
	}
}

func TestAnExceptionThatIsExpiredTooDistantOrIncompleteFails(t *testing.T) {
	vulns := map[string][]string{"kubevirt.io/kubevirt@1.9.1": {"GO-2026-3333"}}
	for name, entry := range map[string]exception{
		"expired":            excepted("GO-2026-3333", "tool:virtctl@1.9.1", "2026-11-09"),
		"too distant":        excepted("GO-2026-3333", "tool:virtctl@1.9.1", "2027-02-09"),
		"no reason":          {ID: "GO-2026-3333", Component: "tool:virtctl@1.9.1", State: "not_affected", Expires: "2027-01-01"},
		"bad state":          {ID: "GO-2026-3333", Component: "tool:virtctl@1.9.1", State: "accepted", Reason: "r", Expires: "2027-01-01"},
		"no justification":   {ID: "GO-2026-3333", Component: "tool:virtctl@1.9.1", State: "not_affected", Reason: "r", Expires: "2027-01-01"},
		"a justified no_fix": {ID: "GO-2026-3333", Component: "tool:virtctl@1.9.1", State: "no_fix", Justification: "code_not_present", Reason: "r", Expires: "2027-01-01"},
	} {
		f := newFixture(t, vulns, scanned, []exception{entry})
		if err := run(f.options, &strings.Builder{}); err == nil {
			t.Errorf("an exception that is %s passed", name)
		}
	}
	f := newFixture(t, vulns, scanned, []exception{excepted("GO-2026-3333", "tool:virtctl@1.9.1", "2027-02-08")})
	if err := run(f.options, &strings.Builder{}); err != nil {
		t.Fatalf("an exception at the 90-day bound failed: %v", err)
	}
}

func TestAScanWithoutItsConfigurationIsRefused(t *testing.T) {
	f := newFixture(t, nil, `{"finding":{"osv":"GO-1","trace":[]}}`+"\n", nil)
	if err := run(f.options, &strings.Builder{}); err == nil {
		t.Fatal("a stream that is no govulncheck scan passed")
	}
}

func TestAClientIsJudgedByTheModulesItsBinaryNames(t *testing.T) {
	vulns := map[string][]string{"helm.sh/helm/v4@4.3.1": {"GO-2026-9999"}}
	f := newClientFixture(t, vulns, scanned, nil, map[string]string{"helm": namedModules + moduleOnly + symbolFinding, "virtctl": stdlibOnly})
	var out strings.Builder
	err := run(f.options, &out)
	if err == nil || !strings.Contains(out.String(), "GO-2026-6603 in tool:helm@4.3.1 (golang.org/x/net@v0.57.0, fixed in v0.60.0) has no exception") {
		t.Fatalf("a symbol the binary holds passed: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "GO-2025-3521") || strings.Contains(out.String(), "GO-2026-9999") {
		t.Fatalf("a module-level finding or the top-level module of a client whose binary names modules failed the gate:\n%s", out.String())
	}
	covered := newClientFixture(t, vulns, scanned, []exception{excepted("GO-2026-6603", "tool:helm@4.3.1", "2027-01-01")}, map[string]string{"helm": namedModules + moduleOnly + symbolFinding, "virtctl": stdlibOnly})
	if err := run(covered.options, &out); err != nil {
		t.Fatalf("a covered binary finding failed: %v", err)
	}
}

func TestAClientWhoseBinaryNamesNoModuleFallsBackToItsTopLevelModule(t *testing.T) {
	vulns := map[string][]string{"kubevirt.io/kubevirt@1.9.1": {"GO-2026-3333"}}
	f := newClientFixture(t, vulns, scanned, nil, map[string]string{"helm": namedModules, "virtctl": stdlibOnly + strings.Replace(symbolFinding, "golang.org/x/net", "stdlib", 1)})
	var out strings.Builder
	if err := run(f.options, &out); err == nil || !strings.Contains(out.String(), "GO-2026-3333 in tool:virtctl@1.9.1 has no exception") || !strings.Contains(out.String(), "GO-2026-6603 in tool:virtctl@1.9.1 (stdlib@v0.57.0, fixed in v0.60.0) has no exception") {
		t.Fatalf("a module-less client skipped a check: %v\n%s", err, out.String())
	}
}

func TestAClientThatCannotBeScannedFailsTheGate(t *testing.T) {
	f := newClientFixture(t, nil, scanned, nil, map[string]string{"helm": namedModules})
	if err := run(f.options, &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "virtctl") {
		t.Fatalf("a client with no scan passed: %v", err)
	}
	f = newClientFixture(t, nil, scanned, nil, map[string]string{"helm": "not json", "virtctl": stdlibOnly})
	if err := run(f.options, &strings.Builder{}); err == nil {
		t.Fatal("a scan that is no govulncheck stream passed")
	}
}

func develScan(modules string) string {
	return `{"config":{"scan_mode":"binary"}}` + "\n" + `{"SBOM":{"go_version":"go1.26.10","modules":[{"path":"example.com/client","version":"v1.0.0"},` + modules + `]}}` + "\n"
}

func tableOf(t *testing.T, f fixture, document string) {
	t.Helper()
	if err := os.WriteFile(f.options.table, []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAModuleWithNoReleaseVersionIsAskedOfOSVAtTheClientsRelease(t *testing.T) {
	vulns := map[string][]string{"k8s.io/client-go@0.9.1": {"GO-2026-7777"}, "github.com/vmware/govmomi@0.56.2": {"GO-2026-8888"}}
	scans := map[string]string{
		"kubectl": develScan(`{"path":"k8s.io/client-go","version":"(devel)"}`),
		"govc":    develScan(`{"path":"../","version":"(devel)"}`),
	}
	f := newClientFixture(t, vulns, scanned, nil, scans)
	tableOf(t, f, `{"tools":[{"kind":"govc","default":{"version":"0.56.2"}},{"kind":"kubectl","default":{"version":"1.9.1"}}]}`)
	var out strings.Builder
	err := run(f.options, &out)
	for _, want := range []string{"GO-2026-7777 in tool:kubectl@1.9.1 has no exception", "GO-2026-8888 in tool:govc@0.56.2 has no exception"} {
		if err == nil || !strings.Contains(out.String(), want) {
			t.Fatalf("a finding of a module with no release version passed (%q): %v\n%s", want, err, out.String())
		}
	}
	covered := newClientFixture(t, vulns, scanned, []exception{excepted("GO-2026-7777", "tool:kubectl@1.9.1", "2027-01-01"), excepted("GO-2026-8888", "tool:govc@0.56.2", "2027-01-01")}, scans)
	tableOf(t, covered, `{"tools":[{"kind":"govc","default":{"version":"0.56.2"}},{"kind":"kubectl","default":{"version":"1.9.1"}}]}`)
	if err := run(covered.options, &out); err != nil {
		t.Fatalf("covered findings failed: %v", err)
	}
}

func TestAModuleWithNoReleaseVersionThatNoTableMapsIsRefused(t *testing.T) {
	f := newClientFixture(t, nil, scanned, nil, map[string]string{"kubectl": develScan(`{"path":"github.com/other/module","version":""}`)})
	tableOf(t, f, `{"tools":[{"kind":"kubectl","default":{"version":"1.9.1"}}]}`)
	if err := run(f.options, &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "cannot map") {
		t.Fatalf("an unmapped module with no release version was skipped: %v", err)
	}
}
