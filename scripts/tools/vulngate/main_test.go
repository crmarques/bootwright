package main

import (
	"encoding/json"
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

func newFixture(t *testing.T, vulns map[string][]string, govulncheck string, entries []exception) fixture {
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
	vulns := map[string][]string{"helm.sh/helm/v4@4.3.1": {"GO-2026-3333"}}
	for name, entry := range map[string]exception{
		"expired":            excepted("GO-2026-3333", "tool:helm@4.3.1", "2026-11-09"),
		"too distant":        excepted("GO-2026-3333", "tool:helm@4.3.1", "2027-02-09"),
		"no reason":          {ID: "GO-2026-3333", Component: "tool:helm@4.3.1", State: "not_affected", Expires: "2027-01-01"},
		"bad state":          {ID: "GO-2026-3333", Component: "tool:helm@4.3.1", State: "accepted", Reason: "r", Expires: "2027-01-01"},
		"no justification":   {ID: "GO-2026-3333", Component: "tool:helm@4.3.1", State: "not_affected", Reason: "r", Expires: "2027-01-01"},
		"a justified no_fix": {ID: "GO-2026-3333", Component: "tool:helm@4.3.1", State: "no_fix", Justification: "code_not_present", Reason: "r", Expires: "2027-01-01"},
	} {
		f := newFixture(t, vulns, scanned, []exception{entry})
		if err := run(f.options, &strings.Builder{}); err == nil {
			t.Errorf("an exception that is %s passed", name)
		}
	}
	f := newFixture(t, vulns, scanned, []exception{excepted("GO-2026-3333", "tool:helm@4.3.1", "2027-02-08")})
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
