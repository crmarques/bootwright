package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

var authorizationTokenTablePublications = []string{
	filepath.Join("..", "..", "specs", "state-model.md"),
	filepath.Join("..", "..", "specs", "adr", "0030-one-intent-flag-and-named-authorizations.md"),
	filepath.Join("..", "..", "docs", "advanced", "operations.md"),
}

var authorizationTableRow = regexp.MustCompile("^\\s*\\|\\s*`([a-z][a-z-]*)`\\s*\\|")

const authorizationAcceptedByColumn = "accepted by"

var authorizationPublishedVerbs = map[string]string{
	"apply":                           authorizeVerbApply,
	"plan":                            authorizeVerbApply,
	"destroy":                         authorizeVerbDestroy,
	"storage-cluster replace-arbiter": authorizeVerbReplaceArbiter,
}

type publishedAuthorizationRow struct {
	token string
	line  int
	cells []string
}

type publishedAuthorizationTable struct {
	path    string
	columns []string
	rows    []publishedAuthorizationRow
}

func TestAuthorizationVocabularyMatchesPublishedContract(t *testing.T) {
	code := authorizationTokenNames()
	sort.Strings(code)
	for _, path := range authorizationTokenTablePublications {
		published := readAuthorizationTokenTable(t, path)
		if len(published) == 0 {
			t.Fatalf("%s carries no `| token | authorizes |` table; the --authorize vocabulary must stay published there", path)
		}
		for _, name := range code {
			if !slices.Contains(published, name) {
				t.Errorf("%s does not document the --authorize token %q; a token must be published wherever the vocabulary is stated", path, name)
			}
		}
		for _, name := range published {
			if !slices.Contains(code, name) {
				t.Errorf("%s documents the --authorize token %q, which internal/cli/authorize.go does not define; remove it or implement it", path, name)
			}
		}
	}
}

func readAuthorizationTokenTable(t *testing.T, path string) []string {
	t.Helper()
	var out []string
	for _, row := range readAuthorizationTokenRows(t, path).rows {
		out = append(out, row.token)
	}
	sort.Strings(out)
	return out
}

func readAuthorizationTokenRows(t *testing.T, path string) publishedAuthorizationTable {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	table := publishedAuthorizationTable{path: path}
	inTable := false
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "| token | authorizes |") {
			inTable = true
			table.columns = markdownRowCells(trimmed)
			continue
		}
		if !inTable {
			continue
		}
		if strings.HasPrefix(trimmed, "| ---") {
			continue
		}
		match := authorizationTableRow.FindStringSubmatch(line)
		if match == nil {
			inTable = false
			continue
		}
		table.rows = append(table.rows, publishedAuthorizationRow{token: match[1], line: i + 1, cells: markdownRowCells(trimmed)})
	}
	return table
}

func markdownRowCells(line string) []string {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 2 || !strings.HasPrefix(trimmed, "|") || !strings.HasSuffix(trimmed, "|") {
		return nil
	}
	parts := strings.Split(trimmed[1:len(trimmed)-1], "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

func (tbl publishedAuthorizationTable) columnIndex(name string) int {
	for i, column := range tbl.columns {
		if strings.EqualFold(column, name) {
			return i
		}
	}
	return -1
}

func TestAuthorizationAcceptedByColumnMatchesTheConsumingVerbs(t *testing.T) {
	published := 0
	for _, path := range authorizationTokenTablePublications {
		table := readAuthorizationTokenRows(t, path)
		if len(table.rows) == 0 {
			t.Fatalf("%s carries no `| token | authorizes |` table; the --authorize vocabulary must stay published there", path)
		}
		column := table.columnIndex(authorizationAcceptedByColumn)
		if column < 0 {
			continue
		}
		published++
		compared := 0
		for _, row := range table.rows {
			token, known := authorizationTokenByName(row.token)
			if !known {
				continue
			}
			compared++
			if column >= len(row.cells) {
				t.Errorf("%s:%d the row for --authorize %s has %d cells, so it publishes no %q column value", path, row.line, row.token, len(row.cells), authorizationAcceptedByColumn)
				continue
			}
			verbs, err := parsePublishedAcceptedBy(row.cells[column])
			if err != nil {
				t.Errorf("%s:%d the %q cell of --authorize %s is unreadable: %v; spell each verb as one of %s", path, row.line, authorizationAcceptedByColumn, row.token, err, strings.Join(publishedVerbLabels(), ", "))
				continue
			}
			want := append([]string(nil), token.verbs...)
			sort.Strings(want)
			if !slices.Equal(verbs, want) {
				t.Errorf("%s:%d the %q cell of --authorize %s publishes %v, but internal/cli/authorize.go declares verbs %v; the column is normative, so the table and the token registry must agree", path, row.line, authorizationAcceptedByColumn, row.token, verbs, want)
			}
		}
		if compared != len(authorizationTokens) {
			t.Errorf("%s compared the %q cell of %d tokens, but internal/cli/authorize.go defines %d; the column must cover every token the table publishes", path, authorizationAcceptedByColumn, compared, len(authorizationTokens))
		}
	}
	if published == 0 {
		t.Fatalf("no publication in %v carries an %q column; the per-token verb set must stay published somewhere", authorizationTokenTablePublications, authorizationAcceptedByColumn)
	}
}

func parsePublishedAcceptedBy(cell string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(cell, ",") {
		label := strings.TrimSpace(strings.Trim(strings.TrimSpace(part), "`"))
		if label == "" {
			continue
		}
		verb, known := authorizationPublishedVerbs[label]
		if !known {
			return nil, fmt.Errorf("%q names no authorization verb", label)
		}
		if !slices.Contains(out, verb) {
			out = append(out, verb)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("it names no verb at all")
	}
	sort.Strings(out)
	return out, nil
}

func publishedVerbLabels() []string {
	out := make([]string, 0, len(authorizationPublishedVerbs))
	for label := range authorizationPublishedVerbs {
		out = append(out, "`"+label+"`")
	}
	sort.Strings(out)
	return out
}

func TestEveryAuthorizationTokenHasASafetyMatrixCase(t *testing.T) {
	exercised := map[string]map[string]bool{}
	for _, tc := range safetyMatrixCases() {
		verb := safetyCaseAuthorizationVerb(tc.args)
		if verb == "" {
			continue
		}
		if exercised[verb] == nil {
			exercised[verb] = map[string]bool{}
		}
		for _, name := range safetyCaseAuthorizationNames(tc.args) {
			exercised[verb][name] = true
		}
	}
	for _, token := range authorizationTokens {
		for _, verb := range token.verbs {
			if !exercised[verb][token.name] {
				t.Errorf("no case in safetyMatrixCases() passes --authorize %s to %s; every verb that accepts a token must exercise it in the scenario matrix", token.name, verb)
			}
		}
	}
}

func safetyCaseAuthorizationVerb(args []string) string {
	args = stripLeadingGlobalFlags(args)
	if len(args) == 0 {
		return ""
	}
	switch args[0] {
	case "apply":
		return authorizeVerbApply
	case "destroy":
		return authorizeVerbDestroy
	case "storage-cluster":
		if len(args) > 1 && args[1] == "replace-arbiter" {
			return authorizeVerbReplaceArbiter
		}
	}
	return ""
}

func safetyCaseAuthorizationNames(args []string) []string {
	var names []string
	for i, arg := range args {
		value := ""
		switch {
		case arg == "--authorize" && i+1 < len(args):
			value = args[i+1]
		case strings.HasPrefix(arg, "--authorize="):
			value = strings.TrimPrefix(arg, "--authorize=")
		}
		for _, name := range strings.Split(value, ",") {
			if name = strings.TrimSpace(name); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

func TestEveryAuthorizationTokenDeclaresAConsumingVerb(t *testing.T) {
	for _, token := range authorizationTokens {
		if len(token.verbs) == 0 {
			t.Errorf("--authorize %s declares no consuming verb; a token no verb can consume must be removed, not published", token.name)
			continue
		}
		for _, verb := range token.verbs {
			if !slices.Contains(authorizationVerbs(), verb) {
				t.Errorf("--authorize %s declares unknown verb %q", token.name, verb)
			}
		}
		if token.inert == "" {
			t.Errorf("--authorize %s has no inert reason; an unconsumed token must say why it had no effect", token.name)
		}
		if len(token.verbs) == len(authorizationVerbs()) {
			continue
		}
		if token.elsewhere == "" {
			t.Errorf("--authorize %s is accepted by only %v, so it must carry the guidance printed when another verb is given it", token.name, token.verbs)
		}
	}
}

func TestAuthorizationTokenRejectedOnAVerbThatCannotConsumeIt(t *testing.T) {
	for _, token := range authorizationTokens {
		for _, verb := range authorizationVerbs() {
			if slices.Contains(token.verbs, verb) {
				if _, err := parseAuthorizations([]string{token.name}, verb); err != nil {
					t.Errorf("--authorize %s must be accepted by %s: %v", token.name, verb, err)
				}
				continue
			}
			_, err := parseAuthorizations([]string{token.name}, verb)
			if err == nil {
				t.Errorf("--authorize %s is not consumable by %s, so it must be refused as a usage error rather than silently ignored", token.name, verb)
				continue
			}
			if !strings.Contains(err.Error(), token.elsewhere) {
				t.Errorf("refusing --authorize %s on %s must name what to do instead, got %q", token.name, verb, err.Error())
			}
		}
	}
}
