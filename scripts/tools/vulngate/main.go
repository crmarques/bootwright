// Command vulngate fails a build on a known vulnerability in what the
// executable carries or pins. It reads govulncheck's JSON for the
// executable's own Go code, keeping only findings whose trace reaches a
// function, and asks OSV about every locked wheel and every default client of
// the tool table. Each finding needs an exception that names it, its
// component and a reason and expires within 90 days; an expired, too distant
// or unused exception fails as well. On success it writes the verdict the
// build stamps into the executable.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	exceptionsFormat = "bootwright.vulnerability-exceptions-v1"
	maxValidity      = 90 * 24 * time.Hour
	maxResponse      = 16 << 20
)

type finding struct{ ID, Ref string }

type exception struct {
	ID            string   `json:"id"`
	Aliases       []string `json:"aliases"`
	Component     string   `json:"component"`
	State         string   `json:"state"`
	Justification string   `json:"justification"`
	Reason        string   `json:"reason"`
	Expires       string   `json:"expires"`
}

type exceptions struct {
	Format     string      `json:"format"`
	Exceptions []exception `json:"exceptions"`
}

type lockFile struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Filename string `json:"filename"`
}

type lock struct {
	Installer lockFile              `json:"installer"`
	ABIs      map[string][]lockFile `json:"abis"`
}

type pin struct {
	Kind    string `json:"kind"`
	Default struct {
		Version string `json:"version"`
	} `json:"default"`
}

type table struct {
	Tools []pin `json:"tools"`
}

type options struct {
	govulncheck, lock, table, exceptions, verdict, osv string
	today                                              time.Time
	client                                             *http.Client
}

var releaseVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

var justifications = []string{"code_not_present", "code_not_reachable", "requires_configuration", "requires_dependency", "requires_environment", "protected_by_compiler", "protected_at_runtime", "protected_at_perimeter", "protected_by_mitigating_control"}

var toolModules = map[string]string{
	"govc":    "github.com/vmware/govmomi",
	"kubectl": "k8s.io/kubernetes",
	"virtctl": "kubevirt.io/kubevirt",
}

func main() {
	var o options
	today := ""
	flag.StringVar(&o.govulncheck, "govulncheck", "", "govulncheck -format json output for the executable's packages")
	flag.StringVar(&o.lock, "lock", "", "the controller runtime lock")
	flag.StringVar(&o.table, "table", "", "the controller tool table")
	flag.StringVar(&o.exceptions, "exceptions", "", "the reviewed vulnerability exceptions")
	flag.StringVar(&o.verdict, "verdict", "", "where to write the verdict the build stamps")
	flag.StringVar(&o.osv, "osv", "https://api.osv.dev", "the OSV API base")
	flag.StringVar(&today, "today", "", "the date to judge expiry by, YYYY-MM-DD; today when empty")
	flag.Parse()
	o.today = time.Now().UTC().Truncate(24 * time.Hour)
	if today != "" {
		parsed, err := time.Parse(time.DateOnly, today)
		if err != nil {
			fail(err)
		}
		o.today = parsed
	}
	o.client = &http.Client{Timeout: 2 * time.Minute}
	if err := run(o, os.Stdout); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "vulngate:", err)
	os.Exit(1)
}

func run(o options, out io.Writer) error {
	if o.govulncheck == "" || o.lock == "" || o.table == "" || o.exceptions == "" || o.verdict == "" {
		return errors.New("-govulncheck, -lock, -table, -exceptions and -verdict are required")
	}
	if err := os.Remove(o.verdict); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	allowed, err := readExceptions(o.exceptions, o.today)
	if err != nil {
		return err
	}
	found, err := goFindings(o.govulncheck)
	if err != nil {
		return err
	}
	queries, refs, err := osvQueries(o.lock, o.table)
	if err != nil {
		return err
	}
	pinned, err := queryOSV(o, queries, refs)
	if err != nil {
		return err
	}
	found = append(found, pinned...)
	failures := judge(found, allowed)
	for _, line := range failures {
		fmt.Fprintln(out, line)
	}
	if len(failures) != 0 {
		return fmt.Errorf("%d known vulnerabilities or exceptions need a decision; .agents/skills/code-implementation/references/verification.md#vulnerability-findings says how", len(failures))
	}
	verdict := "passed:" + o.today.Format(time.DateOnly)
	fmt.Fprintf(out, "vulngate: %s, %d findings each covered by a reviewed exception\n", verdict, len(found))
	return os.WriteFile(o.verdict, []byte(verdict+"\n"), 0o644)
}

func readExceptions(path string, today time.Time) ([]exception, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document exceptions
	if err := decoder.Decode(&document); err != nil || document.Format != exceptionsFormat {
		return nil, fmt.Errorf("%s is no %s document: %v", path, exceptionsFormat, err)
	}
	for _, entry := range document.Exceptions {
		expires, err := time.Parse(time.DateOnly, entry.Expires)
		switch {
		case entry.ID == "" || entry.Component == "" || strings.TrimSpace(entry.Reason) == "":
			return nil, fmt.Errorf("an exception for %q names no advisory, component or reason", entry.ID)
		case entry.State != "not_affected" && entry.State != "no_fix":
			return nil, fmt.Errorf("the exception for %s is %q, not not_affected or no_fix", entry.ID, entry.State)
		case (entry.State == "not_affected") != slices.Contains(justifications, entry.Justification):
			return nil, fmt.Errorf("the exception for %s states %s with justification %q; not_affected takes one of %v and no_fix none", entry.ID, entry.State, entry.Justification, justifications)
		case err != nil:
			return nil, fmt.Errorf("the exception for %s expires on no YYYY-MM-DD date", entry.ID)
		case expires.Before(today):
			return nil, fmt.Errorf("the exception for %s expired on %s; decide it anew", entry.ID, entry.Expires)
		case expires.Sub(today) > maxValidity:
			return nil, fmt.Errorf("the exception for %s expires on %s, more than 90 days after %s", entry.ID, entry.Expires, today.Format(time.DateOnly))
		}
	}
	return document.Exceptions, nil
}

func goFindings(path string) ([]finding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var found []finding
	configured := false
	for {
		var message struct {
			Config  *json.RawMessage `json:"config"`
			Finding *struct {
				OSV   string `json:"osv"`
				Trace []struct {
					Module   string `json:"module"`
					Version  string `json:"version"`
					Function string `json:"function"`
				} `json:"trace"`
			} `json:"finding"`
		}
		if err := decoder.Decode(&message); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("%s is no govulncheck JSON stream: %w", path, err)
		}
		configured = configured || message.Config != nil
		if message.Finding == nil || len(message.Finding.Trace) == 0 || message.Finding.Trace[0].Function == "" {
			continue
		}
		frame := message.Finding.Trace[0]
		ref := "go:" + frame.Module + "@" + frame.Version
		if frame.Module == "stdlib" {
			ref = "go:stdlib@" + strings.TrimPrefix(frame.Version, "v")
		}
		if !slices.Contains(found, finding{message.Finding.OSV, ref}) {
			found = append(found, finding{message.Finding.OSV, ref})
		}
	}
	if !configured {
		return nil, fmt.Errorf("%s holds no govulncheck scan", path)
	}
	return found, nil
}

type osvQuery struct {
	Package struct {
		Ecosystem string `json:"ecosystem"`
		Name      string `json:"name"`
	} `json:"package"`
	Version string `json:"version"`
}

func osvQueries(lockPath, tablePath string) ([]osvQuery, [][]string, error) {
	var locked lock
	var pins table
	for path, target := range map[string]any{lockPath: &locked, tablePath: &pins} {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		if err := json.Unmarshal(data, target); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	files := []lockFile{locked.Installer}
	for _, abi := range slices.Sorted(func(yield func(string) bool) {
		for abi := range locked.ABIs {
			if !yield(abi) {
				return
			}
		}
	}) {
		files = append(files, locked.ABIs[abi]...)
	}
	var queries []osvQuery
	var refs [][]string
	index := map[string]int{}
	for _, file := range files {
		key := file.Name + "@" + file.Version
		if position, seen := index[key]; seen {
			if !slices.Contains(refs[position], "pypi:"+file.Filename) {
				refs[position] = append(refs[position], "pypi:"+file.Filename)
			}
			continue
		}
		index[key] = len(queries)
		query := osvQuery{Version: file.Version}
		query.Package.Ecosystem, query.Package.Name = "PyPI", file.Name
		queries = append(queries, query)
		refs = append(refs, []string{"pypi:" + file.Filename})
	}
	for _, entry := range pins.Tools {
		if !releaseVersion.MatchString(entry.Default.Version) {
			return nil, nil, fmt.Errorf("the tool table's default %s %q is no MAJOR.MINOR.PATCH, so its module cannot be named", entry.Kind, entry.Default.Version)
		}
		module := toolModules[entry.Kind]
		if entry.Kind == "helm" {
			major, _, _ := strings.Cut(entry.Default.Version, ".")
			module = "helm.sh/helm/v" + major
		}
		if module == "" {
			return nil, nil, fmt.Errorf("the tool table pins %s, whose Go module this gate does not know", entry.Kind)
		}
		query := osvQuery{Version: entry.Default.Version}
		query.Package.Ecosystem, query.Package.Name = "Go", module
		queries = append(queries, query)
		refs = append(refs, []string{"tool:" + entry.Kind + "@" + entry.Default.Version})
	}
	return queries, refs, nil
}

func queryOSV(o options, queries []osvQuery, refs [][]string) ([]finding, error) {
	body, err := json.Marshal(map[string]any{"queries": queries})
	if err != nil {
		return nil, err
	}
	response, err := o.client.Post(strings.TrimSuffix(o.osv, "/")+"/v1/querybatch", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("OSV could not be asked; VULNCHECK=skip builds an executable stamped unchecked: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OSV answered %s", response.Status)
	}
	var answer struct {
		Results []struct {
			Vulns []struct {
				ID string `json:"id"`
			} `json:"vulns"`
			NextPageToken string `json:"next_page_token"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponse)).Decode(&answer); err != nil {
		return nil, err
	}
	if len(answer.Results) != len(queries) {
		return nil, fmt.Errorf("OSV answered %d of %d queries", len(answer.Results), len(queries))
	}
	var found []finding
	for position, result := range answer.Results {
		if result.NextPageToken != "" {
			return nil, fmt.Errorf("OSV paged its answer for %s, which this gate does not follow", queries[position].Package.Name)
		}
		for _, vuln := range result.Vulns {
			for _, ref := range refs[position] {
				found = append(found, finding{vuln.ID, ref})
			}
		}
	}
	return found, nil
}

func judge(found []finding, allowed []exception) []string {
	var failures []string
	used := make([]bool, len(allowed))
	for _, item := range found {
		index := slices.IndexFunc(allowed, func(entry exception) bool {
			return entry.Component == item.Ref && (entry.ID == item.ID || slices.Contains(entry.Aliases, item.ID))
		})
		if index < 0 {
			failures = append(failures, "known vulnerability "+item.ID+" in "+item.Ref+" has no exception")
			continue
		}
		used[index] = true
	}
	for index, entry := range allowed {
		if !used[index] {
			failures = append(failures, "the exception for "+entry.ID+" in "+entry.Component+" matches no finding; remove it")
		}
	}
	slices.Sort(failures)
	return failures
}
