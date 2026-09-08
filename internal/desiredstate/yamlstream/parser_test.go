package yamlstream_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
)

func TestParserPreservesAuthoredScalarsAndProvenance(t *testing.T) {
	data := "plain: 18446744073709551616\nquoted: \"true\"\nsingle: '12'\nexplicit: !!str false\nliteral: |\n  first\n  second\nfolded: >-\n  first\n  second\nflow: [true, false]\n"
	documents := parseOK(t, sources("input.yaml", data))
	if len(documents) != 1 || documents[0].Path != "input.yaml" || documents[0].Index != 1 {
		t.Fatalf("unexpected documents: %+v", documents)
	}
	root := documents[0].Root
	if root.Kind != desiredstate.DocumentKind || root.Content[0].Kind != desiredstate.MappingKind {
		t.Fatalf("unexpected document shape: %+v", root)
	}
	fields := root.Content[0].Content
	cases := []struct {
		name, value string
		style       desiredstate.Style
		explicit    bool
		line        int
	}{
		{"plain", "18446744073709551616", desiredstate.PlainStyle, false, 1},
		{"quoted", "true", desiredstate.DoubleQuotedStyle, false, 2},
		{"single", "12", desiredstate.SingleQuotedStyle, false, 3},
		{"explicit", "false", desiredstate.PlainStyle, true, 4},
		{"literal", "first\nsecond\n", desiredstate.LiteralStyle, false, 5},
		{"folded", "first second", desiredstate.FoldedStyle, false, 8},
		{"flow", "", desiredstate.FlowStyle, false, 11},
	}
	for i, want := range cases {
		key, value := fields[2*i], fields[2*i+1]
		if key.Value != want.name || value.Value != want.value || value.Style != want.style || value.ExplicitTag != want.explicit || value.Line != want.line || value.Column < 1 {
			t.Errorf("field %s: key=%+v value=%+v", want.name, key, value)
		}
	}
	if fields[7].Tag != "!!str" {
		t.Errorf("explicit tag was not preserved: %+v", fields[7])
	}
}

func TestParserDistinguishesEmptyDocumentsAndExplicitValues(t *testing.T) {
	cases := []struct {
		name, data string
		documents  int
		empty      bool
	}{
		{"empty stream", "", 0, false},
		{"comments", "# comment\n", 0, false},
		{"empty document", "---\n# comment\n", 1, true},
		{"explicit end", "---\n...\n", 1, true},
		{"null", "---\nnull\n", 1, false},
		{"tilde", "---\n~\n", 1, false},
		{"tagged empty null", "--- !!null\n", 1, false},
		{"tagged quoted null", "--- !!null \"\"\n", 1, false},
		{"quoted empty", "--- \"\"\n", 1, false},
		{"anchored empty", "--- &a\n", 1, false},
		{"non-specific tag", "--- !\n", 1, false},
		{"anchored non-specific tag", "--- &a !\n", 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			documents := parseOK(t, sources("input.yaml", tc.data))
			if len(documents) != tc.documents {
				t.Fatalf("documents=%d, want %d", len(documents), tc.documents)
			}
			if len(documents) != 0 && documents[0].IsEmpty() != tc.empty {
				t.Fatalf("IsEmpty=%v, want %v; root=%+v", documents[0].IsEmpty(), tc.empty, documents[0].Root.Content[0])
			}
		})
	}
}

func TestParserPreservesNonSpecificTags(t *testing.T) {
	cases := []string{
		"value: ! true\n",
		"value: &a ! true\n",
		"value: ! &a true\n",
		"value: &a\n  # comment\n  ! true\n",
		"é: other\nvalue: ! true\n",
		"{é: other, value: ! true}\n",
		"\ufeffvalue: ! true\n",
		"é: other\r\nvalue: ! true\r\n",
		"é: other\rvalue: ! true\r",
		"é: other\u0085value: ! true\u0085",
		"é: other\u2028value: ! true\u2028",
		"é: other\u2029value: ! true\u2029",
		"value: &a\u0085  # comment\u0085  ! true\u0085",
	}
	for _, data := range cases {
		t.Run(data, func(t *testing.T) {
			documents := parseOK(t, sources("input.yaml", data))
			fields := documents[0].Root.Content[0].Content
			value := fields[len(fields)-1]
			if !value.ExplicitTag || value.Tag != "!!str" || value.Value != "true" {
				t.Fatalf("lost non-specific scalar tag: %+v", value)
			}
		})
	}
}

func TestParserPreservesRejectedGrammarWithoutExpansion(t *testing.T) {
	data := "duplicate: first\nduplicate: second\nanchor: &a {self: *a}\nmerge: {<<: *a}\ncustom: !custom value\nnull: null\nbinary: !!binary YQ==\ntime: 2026-01-01\n"
	documents := parseOK(t, sources("input.yaml", data))
	fields := documents[0].Root.Content[0].Content
	if fields[0].Value != fields[2].Value || fields[1].Value != "first" || fields[3].Value != "second" {
		t.Fatal("duplicate entries were lost")
	}
	anchor := fields[5]
	alias := anchor.Content[1]
	if anchor.Anchor != "a" || alias.Kind != desiredstate.AliasKind || alias.Value != "a" || len(alias.Content) != 0 {
		t.Fatalf("alias was expanded or provenance lost: anchor=%+v alias=%+v", anchor, alias)
	}
	if fields[9].Tag != "!custom" || !fields[9].ExplicitTag || fields[11].Tag != "!!null" || fields[13].Tag != "!!binary" || fields[15].Tag != "!!timestamp" {
		t.Fatal("grammar tags were lost")
	}
}

func TestParserReleasesCompletedTreesWithoutBreakingLaterAliases(t *testing.T) {
	data := "---\n&a {value: first}\n---\n*b\n"
	documents, diagnostics, err := (yamlstream.Parser{}).Parse(context.Background(), sources("input.yaml", data))
	if err != nil || len(documents) != 1 || len(diagnostics) != 1 || diagnostics[0].Source.Document != 2 {
		t.Fatalf("unexpected unknown-anchor result: docs=%d diagnostics=%+v err=%v", len(documents), diagnostics, err)
	}
	data = "---\n&a {value: first}\n---\n*a\n---\n&b {nested: *a}\n---\n*b\n"
	documents = parseOK(t, sources("input.yaml", data))
	if len(documents) != 4 || documents[0].Root.Content[0].Content[1].Value != "first" || documents[1].Root.Content[0].Kind != desiredstate.AliasKind || documents[2].Root.Content[0].Content[1].Kind != desiredstate.AliasKind {
		t.Fatal("conversion changed retained documents or later alias syntax")
	}
}

func TestParserStopsMalformedFileAndContinuesLexically(t *testing.T) {
	files := append(sources("z.yaml", "value: final\n"), sources("a.yaml", "value: first\n---\n[\n---\nvalue: hidden\n")...)
	documents, diagnostics, err := (yamlstream.Parser{}).Parse(context.Background(), files)
	if err != nil || len(documents) != 2 || len(diagnostics) != 1 {
		t.Fatalf("docs=%d diagnostics=%+v err=%v", len(documents), diagnostics, err)
	}
	if documents[0].Path != "a.yaml" || documents[1].Path != "z.yaml" || diagnostics[0].Code != "yaml.syntax" || diagnostics[0].Source.Document != 2 {
		t.Fatalf("wrong processing order: documents=%+v diagnostics=%+v", documents, diagnostics)
	}
	if files[0].Path() != "z.yaml" {
		t.Fatal("parser reordered the caller's files")
	}
}

func TestParserNeverDisclosesSourceInSyntaxDiagnostics(t *testing.T) {
	for _, data := range []string{"*sensitive-synthetic-marker\n", "!!sensitive-synthetic-marker [\n", "a: \"\xff\"\n"} {
		documents, diagnostics, err := (yamlstream.Parser{}).Parse(context.Background(), sources("input.yaml", data))
		if err != nil || len(documents) != 0 || len(diagnostics) != 1 || diagnostics[0].Code != "yaml.syntax" {
			t.Fatalf("unexpected syntax result: docs=%d diagnostics=%+v err=%v", len(documents), diagnostics, err)
		}
		if strings.Contains(fmt.Sprint(diagnostics), "sensitive-synthetic-marker") || strings.Contains(fmt.Sprint(diagnostics), "\xff") {
			t.Fatal("diagnostic disclosed authored scalar content")
		}
	}
}

func TestParserVersionDirectiveBoundary(t *testing.T) {
	parseOK(t, sources("input.yaml", "%YAML 1.1\n---\nvalue: true\n"))
	documents, diagnostics, err := (yamlstream.Parser{}).Parse(context.Background(), sources("input.yaml", "%YAML 1.2\n---\nvalue: true\n"))
	if err != nil || len(documents) != 0 || len(diagnostics) != 1 || diagnostics[0].Code != "yaml.syntax" {
		t.Fatalf("unmodified parser directive boundary: docs=%d diagnostics=%+v err=%v", len(documents), diagnostics, err)
	}
}

func TestParserIndependentConcurrentSessions(t *testing.T) {
	parser := yamlstream.Parser{}
	for i := range 4 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			value := fmt.Sprintf("session-%d", i)
			documents, diagnostics, err := parser.Parse(context.Background(), sources("input.yaml", "value: "+value+"\n"))
			if err != nil || len(diagnostics) != 0 || len(documents) != 1 || documents[0].Root.Content[0].Content[1].Value != value {
				t.Fatalf("concurrent parse: docs=%d diagnostics=%+v err=%v", len(documents), diagnostics, err)
			}
		})
	}
}

func TestParserChecksFileCountBeforeCloningSources(t *testing.T) {
	files := make([]desiredstate.SourceFile, desiredstate.MaxFiles+1)
	assertLimit(t, files, "YAML source files", desiredstate.MaxFiles)
}

func TestParserChecksInclusiveByteLimitsBeforeComposition(t *testing.T) {
	full := "#" + strings.Repeat("x", desiredstate.MaxFileBytes-1)
	parseOK(t, sources("input.yaml", full))
	assertLimit(t, sources("input.yaml", full+"x"), "YAML file bytes", desiredstate.MaxFileBytes)
	var files []desiredstate.SourceFile
	for i := range 16 {
		files = append(files, sources(fmt.Sprintf("%02d.yaml", i), full)...)
	}
	parseOK(t, files)
	files = append(files, sources("last.yaml", "x")...)
	assertLimit(t, files, "aggregate YAML bytes", desiredstate.MaxAllFileBytes)
}

func TestParserChecksAllByteLimitsBeforeAnyComposition(t *testing.T) {
	files := sources("a.yaml", "valid: first\n---\n[\n")
	files = append(files, sources("z.yaml", strings.Repeat("x", desiredstate.MaxFileBytes+1))...)
	assertLimit(t, files, "YAML file bytes", desiredstate.MaxFileBytes)

	full := "#" + strings.Repeat("x", desiredstate.MaxFileBytes-1)
	files = sources("a.yaml", "valid: first\n---\n[\n")
	for i := range 16 {
		files = append(files, sources(fmt.Sprintf("z%02d.yaml", i), full)...)
	}
	assertLimit(t, files, "aggregate YAML bytes", desiredstate.MaxAllFileBytes)
}

func TestParserChecksInclusiveDocumentLimits(t *testing.T) {
	full := strings.Repeat("---\n", desiredstate.MaxFileDocuments)
	if documents := parseOK(t, sources("input.yaml", full)); len(documents) != desiredstate.MaxFileDocuments {
		t.Fatalf("got %d documents", len(documents))
	}
	assertLimit(t, sources("input.yaml", full+"---\n"), "YAML documents per file", desiredstate.MaxFileDocuments)
	var files []desiredstate.SourceFile
	for i := range 32 {
		files = append(files, sources(fmt.Sprintf("%02d.yaml", i), full)...)
	}
	if documents := parseOK(t, files); len(documents) != desiredstate.MaxDocuments {
		t.Fatalf("got %d aggregate documents", len(documents))
	}
	files = append(files, sources("last.yaml", "---\n")...)
	assertLimit(t, files, "aggregate YAML documents", desiredstate.MaxDocuments)
}

func TestParserSyntaxFailurePrecedesOverLimitDocumentCheck(t *testing.T) {
	data := strings.Repeat("---\n", desiredstate.MaxFileDocuments) + "---\n[\n"
	documents, diagnostics, err := (yamlstream.Parser{}).Parse(context.Background(), sources("input.yaml", data))
	if err != nil || len(documents) != desiredstate.MaxFileDocuments || len(diagnostics) != 1 || diagnostics[0].Code != "yaml.syntax" || diagnostics[0].Source.Document != desiredstate.MaxFileDocuments+1 {
		t.Fatalf("unexpected precedence: docs=%d diagnostics=%+v err=%v", len(documents), diagnostics, err)
	}
}

func TestParserChecksDepthIncludingScalarAndMappingKeyEdges(t *testing.T) {
	for _, value := range []string{"value", "{key: value}"} {
		depth := desiredstate.MaxDepth - 1
		if strings.HasPrefix(value, "{") {
			depth--
		}
		data := strings.Repeat("[", depth) + value + strings.Repeat("]", depth)
		parseOK(t, sources("input.yaml", data))
		assertLimit(t, sources("input.yaml", "["+data+"]"), "YAML representation depth", desiredstate.MaxDepth)
	}
	parseOK(t, sources("input.yaml", strings.Repeat("- ", desiredstate.MaxDepth-1)+"value\n"))
	assertLimit(t, sources("input.yaml", strings.Repeat("- ", desiredstate.MaxDepth)+"value\n"), "YAML representation depth", desiredstate.MaxDepth)
}

func TestParserCountsMappingKeysAndAliasNodes(t *testing.T) {
	fullMap := "{" + strings.Repeat("key: value,", (desiredstate.MaxDocumentNodes-2)/2) + "}"
	parseOK(t, sources("input.yaml", fullMap))
	assertLimit(t, sources("input.yaml", strings.TrimSuffix(fullMap, "}")+"more: value}"), "YAML representation nodes per document", desiredstate.MaxDocumentNodes)
	fullAliases := "[&a value," + strings.Repeat("*a,", desiredstate.MaxDocumentNodes-3) + "]"
	parseOK(t, sources("input.yaml", fullAliases))
	assertLimit(t, sources("input.yaml", strings.TrimSuffix(fullAliases, "]")+"*a]"), "YAML representation nodes per document", desiredstate.MaxDocumentNodes)
}

func TestParserChecksAggregateRepresentationNodes(t *testing.T) {
	fullDocument := "---\n[" + strings.Repeat("x,", desiredstate.MaxDocumentNodes-2) + "]\n"
	data := strings.Repeat(fullDocument, desiredstate.MaxNodes/desiredstate.MaxDocumentNodes)
	if documents := parseOK(t, sources("input.yaml", data)); len(documents) != 10 {
		t.Fatalf("got %d documents", len(documents))
	}
	assertLimit(t, sources("input.yaml", data+"---\n"), "aggregate YAML representation nodes", desiredstate.MaxNodes)
}

func TestParserLimitsDiscardAllDocumentsAndStopOtherFiles(t *testing.T) {
	files := append(sources("a.yaml", "["), sources("b.yaml", strings.Repeat("- ", desiredstate.MaxDepth)+"x")...)
	files = append(files, sources("c.yaml", "[synthetic-hidden-diagnostic")...)
	documents, diagnostics, err := (yamlstream.Parser{}).Parse(context.Background(), files)
	var failure *desiredstate.Failure
	if documents != nil || !errors.As(err, &failure) || len(diagnostics) != 2 || diagnostics[0].Code != "yaml.syntax" || diagnostics[1].Code != "input.limit" {
		t.Fatalf("docs=%d diagnostics=%+v err=%v", len(documents), diagnostics, err)
	}
}

func TestParserRetainsSyntaxFailuresForResourceSelection(t *testing.T) {
	files := make([]desiredstate.SourceFile, desiredstate.MaxDiagnostics+1)
	for i := range files {
		files[i] = desiredstate.NewSourceFile(fmt.Sprintf("%04d.yaml", i), []byte("["))
	}
	documents, diagnostics, err := (yamlstream.Parser{}).Parse(context.Background(), files)
	if len(documents) != 0 || err != nil || len(diagnostics) != len(files) || diagnostics[len(diagnostics)-1].Code != "yaml.syntax" {
		t.Fatalf("docs=%d diagnostics=%d err=%v", len(documents), len(diagnostics), err)
	}
}

func TestParserCancellationDiscardsPartialDocuments(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	documents, diagnostics, err := (yamlstream.Parser{}).Parse(ctx, sources("input.yaml", "value"))
	if !errors.Is(err, context.Canceled) || documents != nil || len(diagnostics) != 0 {
		t.Fatalf("docs=%d diagnostics=%+v err=%v", len(documents), diagnostics, err)
	}
	ctx, cancel = context.WithDeadline(context.Background(), time.Time{})
	defer cancel()
	_, _, err = (yamlstream.Parser{}).Parse(ctx, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	progress := &cancelAfterChecks{Context: ctx, remaining: 30, cancel: cancel}
	data := "---\nvalue\n---\n\"" + strings.Repeat("x", desiredstate.MaxFileBytes/2) + "\"\n"
	documents, diagnostics, err = (yamlstream.Parser{}).Parse(progress, sources("input.yaml", data))
	if !errors.Is(err, context.Canceled) || documents != nil || len(diagnostics) != 0 || progress.remaining > 0 {
		t.Fatalf("in-progress cancellation: docs=%d diagnostics=%+v err=%v", len(documents), diagnostics, err)
	}
}

type cancelAfterChecks struct {
	context.Context
	remaining int
	cancel    context.CancelFunc
}

func (c *cancelAfterChecks) Err() error {
	c.remaining--
	if c.remaining == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func FuzzParserBoundedInput(f *testing.F) {
	for _, seed := range []string{"", "---\n", "value: 1\n", "[&a {self: *a}]", "--- !!null\n", "é: ! true\n", "[\xff", "---\n[\n---\n"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		files := []desiredstate.SourceFile{desiredstate.NewSourceFile("input.yaml", data)}
		first, diagnostics, err := (yamlstream.Parser{}).Parse(context.Background(), files)
		second, again, repeatedErr := (yamlstream.Parser{}).Parse(context.Background(), files)
		if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(diagnostics, again) || fmt.Sprint(err) != fmt.Sprint(repeatedErr) {
			t.Fatal("parsing is nondeterministic")
		}
		if !reflect.DeepEqual(files[0].Bytes(), data) && len(data) != 0 {
			t.Fatal("parsing mutated source bytes")
		}
		if err != nil && first != nil {
			t.Fatal("terminal failure returned partial documents")
		}
	})
}

func sources(path, data string) []desiredstate.SourceFile {
	return []desiredstate.SourceFile{desiredstate.NewSourceFile(path, []byte(data))}
}

func parseOK(t testing.TB, files []desiredstate.SourceFile) []desiredstate.Document {
	t.Helper()
	documents, diagnostics, err := (yamlstream.Parser{}).Parse(context.Background(), files)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("parse failed: diagnostics=%+v err=%v", diagnostics, err)
	}
	return documents
}

func assertLimit(t testing.TB, files []desiredstate.SourceFile, resource string, ceiling int) {
	t.Helper()
	documents, diagnostics, err := (yamlstream.Parser{}).Parse(context.Background(), files)
	var failure *desiredstate.Failure
	if documents != nil || !errors.As(err, &failure) || len(diagnostics) != 1 || diagnostics[0].Code != "input.limit" || !strings.Contains(diagnostics[0].Message, resource) || !strings.Contains(diagnostics[0].Message, fmt.Sprint(ceiling)) {
		t.Fatalf("expected %s limit %d: docs=%d diagnostics=%+v err=%v", resource, ceiling, len(documents), diagnostics, err)
	}
}
