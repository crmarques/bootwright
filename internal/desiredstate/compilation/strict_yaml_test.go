package compilation_test

import (
	"context"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const strictYAMLProbePath = "/synthetic/probe.yaml"

const strictYAMLToken = "  type: token\n  source:\n    generated:\n      bytes: 32\n"

type strictYAMLRow struct {
	name, probe string
	want        []string
}

type strictYAMLOutcome struct {
	onProbe, elsewhere []string
}

// strictYAMLDeviation is a refusal row known to refuse with other diagnostics
// than the spec names: the backlog item that fixes it and the exact
// diagnostics on the probe it reports until then.
type strictYAMLDeviation struct {
	item string
	got  []string
}

func TestEveryStrictYAMLRuleRefusesWithItsCode(t *testing.T) {
	rows := strictYAMLRefusals()
	outcomes := map[string]strictYAMLOutcome{}
	for _, row := range rows {
		onProbe, elsewhere := strictYAMLCompile(t, row.probe)
		outcomes[row.name] = strictYAMLOutcome{onProbe: onProbe, elsewhere: elsewhere}
	}
	for _, finding := range strictYAMLFindings(rows, outcomes, strictYAMLKnownDeviations()) {
		t.Error(finding)
	}
}

func TestStrictYAMLLedgerHoldsEachDeviationToItsOutcome(t *testing.T) {
	rows := []strictYAMLRow{{name: "refused", want: []string{"api.type $.spec"}}, {name: "deviating", want: []string{"yaml.alias $.spec"}}}
	refused := strictYAMLOutcome{onProbe: []string{"api.type $.spec"}, elsewhere: []string{}}
	recorded := strictYAMLOutcome{onProbe: []string{"yaml.shape $.spec"}, elsewhere: []string{}}
	ledger := map[string]strictYAMLDeviation{"deviating": {item: "B1", got: []string{"yaml.shape $.spec"}}}
	for _, row := range []struct {
		name      string
		deviating strictYAMLOutcome
		known     map[string]strictYAMLDeviation
		want      []string
	}{
		{name: "the recorded outcome", deviating: recorded, known: ledger},
		{name: "an accepted probe", deviating: strictYAMLOutcome{onProbe: []string{}, elsewhere: []string{}}, known: ledger,
			want: []string{"deviating: probe.yaml reported ; its known deviation B1 records yaml.shape $.spec; elsewhere "}},
		{name: "another refusal", deviating: strictYAMLOutcome{onProbe: []string{"api.field $.spec"}, elsewhere: []string{}}, known: ledger,
			want: []string{"deviating: probe.yaml reported api.field $.spec; its known deviation B1 records yaml.shape $.spec; elsewhere "}},
		{name: "a diagnostic elsewhere", deviating: strictYAMLOutcome{onProbe: recorded.onProbe, elsewhere: []string{"api.reference $.spec.ref"}}, known: ledger,
			want: []string{"deviating: probe.yaml reported yaml.shape $.spec; its known deviation B1 records yaml.shape $.spec; elsewhere api.reference $.spec.ref"}},
		{name: "the spec's outcome", deviating: strictYAMLOutcome{onProbe: []string{"yaml.alias $.spec"}, elsewhere: []string{}}, known: ledger,
			want: []string{"deviating now holds: remove its known deviation"}},
		{name: "no ledger", deviating: recorded,
			want: []string{"deviating: probe.yaml reported yaml.shape $.spec; want yaml.alias $.spec; elsewhere "}},
		{name: "a ledger that records acceptance", deviating: strictYAMLOutcome{onProbe: []string{}, elsewhere: []string{}},
			known: map[string]strictYAMLDeviation{"deviating": {item: "B1"}},
			want:  []string{"known deviation \"deviating\" records no refusal; an accepted probe is a gap, not a deviation"}},
		{name: "a ledger that names no row or item", deviating: recorded,
			known: map[string]strictYAMLDeviation{"deviating": ledger["deviating"], "absent": {item: "R6", got: []string{"yaml.shape $"}}},
			want:  []string{"known deviation \"absent\" names no row of the table: remove it", "known deviation \"absent\" names \"R6\", which is not a backlog item"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			got := strictYAMLFindings(rows, map[string]strictYAMLOutcome{"refused": refused, "deviating": row.deviating}, row.known)
			if !slices.Equal(got, row.want) {
				t.Fatalf("findings = %q, want %q", got, row.want)
			}
		})
	}
}

// strictYAMLFindings holds each row to the diagnostics the spec names, or a
// row with a known deviation to exactly the diagnostics that deviation
// records, so that the ledger never excuses an accepted probe.
func strictYAMLFindings(rows []strictYAMLRow, outcomes map[string]strictYAMLOutcome, known map[string]strictYAMLDeviation) []string {
	var findings []string
	for _, row := range rows {
		outcome := outcomes[row.name]
		want := slices.Sorted(slices.Values(row.want))
		holds := slices.Equal(outcome.onProbe, want) && len(outcome.elsewhere) == 0
		deviation, deviating := known[row.name]
		recorded := slices.Sorted(slices.Values(deviation.got))
		switch {
		case deviating && holds:
			findings = append(findings, row.name+" now holds: remove its known deviation")
		case deviating && (!slices.Equal(outcome.onProbe, recorded) || len(outcome.elsewhere) != 0):
			findings = append(findings, row.name+": probe.yaml reported "+strings.Join(outcome.onProbe, ", ")+"; its known deviation "+deviation.item+" records "+strings.Join(recorded, ", ")+"; elsewhere "+strings.Join(outcome.elsewhere, ", "))
		case !deviating && !holds:
			findings = append(findings, row.name+": probe.yaml reported "+strings.Join(outcome.onProbe, ", ")+"; want "+strings.Join(want, ", ")+"; elsewhere "+strings.Join(outcome.elsewhere, ", "))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(known)) {
		deviation := known[name]
		if !slices.ContainsFunc(rows, func(row strictYAMLRow) bool { return row.name == name }) {
			findings = append(findings, "known deviation "+strconv.Quote(name)+" names no row of the table: remove it")
		}
		if !regexp.MustCompile(`^B[0-9]+$`).MatchString(deviation.item) {
			findings = append(findings, "known deviation "+strconv.Quote(name)+" names "+strconv.Quote(deviation.item)+", which is not a backlog item")
		}
		if len(deviation.got) == 0 {
			findings = append(findings, "known deviation "+strconv.Quote(name)+" records no refusal; an accepted probe is a gap, not a deviation")
		}
	}
	return findings
}

func TestStrictYAMLAcceptsWhatTheGrammarPermits(t *testing.T) {
	for _, row := range []strictYAMLRow{
		{name: "flow style", probe: "{apiVersion: bootwright.io/v1alpha1, kind: Secret, metadata: {name: probe}, spec: {type: token, source: {generated: {bytes: 32}}}}\n"},
		{name: "comment-only document", probe: "# no desired state here\n---\n" + strictYAMLSecret(strictYAMLToken)},
		{name: "YAML 1.1 directive", probe: "%YAML 1.1\n---\n" + strictYAMLSecret(strictYAMLToken)},
		{name: "explicit string tag", probe: strictYAMLSecret("  type: !!str token\n  source:\n    generated:\n      bytes: 32\n")},
		{name: "integer beyond a machine word", probe: strictYAMLBytes("99999999999999999999999"), want: []string{"api.value $.spec.source.generated.bytes"}},
		{name: "exponent ratio", probe: strictYAMLRatio("5e-1"), want: []string{"api.reference $.spec.clusterRef"}},
		{name: "underflowing ratio", probe: strictYAMLRatio("1e-999"), want: []string{"api.reference $.spec.clusterRef"}},
		{name: "integer ratio", probe: strictYAMLRatio("1"), want: []string{"api.reference $.spec.clusterRef"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			got, elsewhere := strictYAMLCompile(t, row.probe)
			if !slices.Equal(got, slices.Sorted(slices.Values(row.want))) || len(elsewhere) != 0 {
				t.Fatalf("probe.yaml reported %v, want %v; elsewhere %v", got, row.want, elsewhere)
			}
		})
	}
}

func strictYAMLKnownDeviations() map[string]strictYAMLDeviation {
	return map[string]strictYAMLDeviation{}
}

func strictYAMLRefusals() []strictYAMLRow {
	rows := []strictYAMLRow{
		{"sequence document", "- a\n- b\n", []string{"yaml.shape $"}},
		{"scalar document", "text\n", []string{"yaml.shape $"}},
		{"null document", "null\n", []string{"yaml.shape $"}},
		{"null key", strictYAMLSecret("  null: opaque\n"), []string{"yaml.shape $.spec"}},
		{"integer key", strictYAMLSecret("  1: opaque\n"), []string{"yaml.shape $.spec"}},
		{"tagged integer key", strictYAMLSecret("  !!int 1: opaque\n"), []string{"yaml.shape $.spec"}},
		{"anchored integer key", strictYAMLSecret("  &k 1: opaque\n"), []string{"yaml.alias $.spec"}},
		{"missing apiVersion", "kind: Secret\nmetadata:\n  name: probe\nspec:\n" + strictYAMLToken, []string{"api.version $.apiVersion"}},
		{"unsupported apiVersion", "apiVersion: bootwright.io/v1beta1\nkind: Secret\nmetadata:\n  name: probe\nspec:\n" + strictYAMLToken, []string{"api.version $.apiVersion"}},
		{"missing kind", "apiVersion: bootwright.io/v1alpha1\nmetadata:\n  name: probe\nspec:\n" + strictYAMLToken, []string{"api.kind $.kind"}},
		{"unregistered kind", "apiVersion: bootwright.io/v1alpha1\nkind: Widget\nmetadata:\n  name: probe\nspec: {}\n", []string{"api.kind $.kind"}},
		{"retired kind", "apiVersion: bootwright.io/v1alpha1\nkind: InfraComponent\nmetadata:\n  name: probe\nspec: {}\n", []string{"api.kind $.kind"}},
		{"absent metadata", "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nspec:\n" + strictYAMLToken, []string{"api.required $.metadata"}},
		{"absent metadata name", "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata: {}\nspec:\n" + strictYAMLToken, []string{"api.required $.metadata.name"}},
		{"absent spec", "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: probe\n", []string{"api.required $.spec"}},
		{"YAML 1.2 directive", "%YAML 1.2\n---\n" + strictYAMLSecret(strictYAMLToken), []string{"yaml.syntax "}},
		{"non-UTF-8 byte", "# \xff\n" + strictYAMLSecret(strictYAMLToken), []string{"yaml.syntax "}},
		{"syntax error ahead of a valid document", "a: [\n---\n" + strictYAMLSecret(strictYAMLToken), []string{"yaml.syntax "}},
		{"duplicate key", strictYAMLSecret(strictYAMLToken + "  type: token\n"), []string{"yaml.duplicate-key $.spec"}},
		{"anchored value", strictYAMLSecret("  type: opaque\n  source: &s {}\n"), []string{"yaml.alias $.spec.source"}},
		{"alias", "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: &n probe\nspec:\n  type: *n\n", []string{"yaml.alias $.metadata.name", "yaml.alias $.spec.type"}},
		{"anchored key", strictYAMLSecret("  &k type: opaque\n"), []string{"yaml.alias $.spec"}},
		{"alias key", "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: &k type\nspec:\n  *k : opaque\n", []string{"yaml.alias $.metadata.name", "yaml.alias $.spec"}},
		{"plain merge key", strictYAMLSecret("  <<: {type: opaque}\n"), []string{"yaml.alias $.spec"}},
		{"quoted merge key", strictYAMLSecret("  '<<': {type: opaque}\n"), []string{"yaml.alias $.spec"}},
		{"tagged merge key", strictYAMLSecret("  !!merge <<: {type: opaque}\n"), []string{"yaml.alias $.spec"}},
		{"unknown field", strictYAMLSecret("  type: opaque\n  unexpected: value\n"), []string{"api.field $.spec"}},
		{"null capabilities element", strictYAMLMachine("  capabilities: [container-runtime, null]\n  os: {provided: true}\n  access: {local: true}\n"), []string{"api.type $.spec.capabilities[1]"}},
	}
	for _, tag := range []string{"!custom opaque", "!!binary b3BhcXVl", "!!timestamp 2001-12-14"} {
		rows = append(rows, strictYAMLRow{"tag " + tag, strictYAMLSecret("  type: " + tag + "\n"), []string{"yaml.tag $.spec.type"}},
			strictYAMLRow{"key tag " + tag, strictYAMLSecret("  " + tag + ": opaque\n"), []string{"yaml.tag $.spec"}})
	}
	for _, scalar := range []string{"2001-12-14", "1", "3.5"} {
		rows = append(rows, strictYAMLRow{"type " + scalar, strictYAMLSecret("  type: " + scalar + "\n"), []string{"api.type $.spec.type"}})
	}
	for _, null := range []string{"null", "~"} {
		rows = append(rows, strictYAMLRow{"source " + null, strictYAMLSecret("  type: opaque\n  source: " + null + "\n"), []string{"api.type $.spec.source"}})
	}
	for _, boolean := range []string{"'true'", "True", "yes"} {
		rows = append(rows, strictYAMLRow{"provided " + boolean, strictYAMLMachine("  os: {provided: " + boolean + "}\n  access: {local: true}\n"), []string{"api.type $.spec.os.provided"}})
	}
	for _, integer := range []string{"'32'", "3_2", "0x20", "0o40", "3.5"} {
		rows = append(rows, strictYAMLRow{"bytes " + integer, strictYAMLBytes(integer), []string{"api.type $.spec.source.generated.bytes"}})
	}
	for _, ratio := range []string{".inf", ".nan", "0x1", "'0.5'", "1e999"} {
		rows = append(rows, strictYAMLRow{"ratio " + ratio, strictYAMLRatio(ratio), []string{"api.type $.spec.autoscale.targetSizeRatio"}})
	}
	return rows
}

func strictYAMLSecret(spec string) string {
	return "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: probe\nspec:\n" + spec
}

func strictYAMLBytes(value string) string {
	return strictYAMLSecret("  type: token\n  source:\n    generated:\n      bytes: " + value + "\n")
}

func strictYAMLMachine(spec string) string {
	return "apiVersion: bootwright.io/v1alpha1\nkind: Machine\nmetadata:\n  name: probe\nspec:\n" + spec
}

func strictYAMLRatio(value string) string {
	return "apiVersion: bootwright.io/v1alpha1\nkind: StoragePool\nmetadata:\n  name: probe\nspec:\n  clusterRef: missing\n  autoscale:\n    targetSizeRatio: " + value + "\n"
}

// strictYAMLCompile compiles one probe beside a valid Environment and returns
// the sorted code and field of each diagnostic on the probe, and every other
// diagnostic.
func strictYAMLCompile(t *testing.T, probe string) (onProbe, elsewhere []string) {
	t.Helper()
	input := desiredstate.Sources{Files: []desiredstate.SourceFile{
		desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(environmentYAML+controllerYAML)),
		desiredstate.NewSourceFile(strictYAMLProbePath, []byte(probe)),
	}, Roots: []string{"/synthetic"}}
	_, report, err := compiler().Compile(context.Background(), input)
	reported := diagnostics.Of(err)
	if err != nil && len(reported) == 0 {
		t.Fatalf("compilation failed without diagnostics: %v", err)
	}
	if report != nil {
		reported = append(reported, report.Diagnostics...)
	}
	onProbe, elsewhere = []string{}, []string{}
	for _, diagnostic := range reported {
		finding := diagnostic.Code + " " + diagnostic.Field
		if diagnostic.Source != nil && diagnostic.Source.Path == strictYAMLProbePath {
			onProbe = append(onProbe, finding)
		} else {
			elsewhere = append(elsewhere, finding+" ("+diagnostic.Message+")")
		}
	}
	slices.Sort(onProbe)
	return onProbe, elsewhere
}
