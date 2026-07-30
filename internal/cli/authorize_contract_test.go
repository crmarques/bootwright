package cli

import (
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
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out []string
	inTable := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "| token | authorizes |") {
			inTable = true
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
		out = append(out, match[1])
	}
	sort.Strings(out)
	return out
}

func TestEveryAuthorizationTokenHasASafetyMatrixCase(t *testing.T) {
	exercised := map[string]bool{}
	for _, tc := range safetyMatrixCases() {
		for i, arg := range tc.args {
			if arg != "--authorize" || i+1 >= len(tc.args) {
				continue
			}
			for _, name := range strings.Split(tc.args[i+1], ",") {
				exercised[strings.TrimSpace(name)] = true
			}
		}
	}
	for _, name := range authorizationTokenNames() {
		if !exercised[name] {
			t.Errorf("no case in safetyMatrixCases() passes --authorize %s; a token that unblocks a refusal must be exercised by the scenario matrix", name)
		}
	}
}

func TestEveryAuthorizationTokenDeclaresAConsumingVerb(t *testing.T) {
	for _, token := range authorizationTokens {
		if len(token.verbs) == 0 {
			t.Errorf("--authorize %s declares no consuming verb; a token no verb can consume must be removed, not published", token.name)
			continue
		}
		for _, verb := range token.verbs {
			if verb != authorizeVerbApply && verb != authorizeVerbDestroy {
				t.Errorf("--authorize %s declares unknown verb %q", token.name, verb)
			}
		}
		if token.inert == "" {
			t.Errorf("--authorize %s has no inert reason; an unconsumed token must say why it had no effect", token.name)
		}
		if slices.Contains(token.verbs, authorizeVerbApply) && slices.Contains(token.verbs, authorizeVerbDestroy) {
			continue
		}
		if token.elsewhere == "" {
			t.Errorf("--authorize %s is accepted by only %v, so it must carry the guidance printed when another verb is given it", token.name, token.verbs)
		}
	}
}

func TestAuthorizationTokenRejectedOnAVerbThatCannotConsumeIt(t *testing.T) {
	for _, token := range authorizationTokens {
		for _, verb := range []string{authorizeVerbApply, authorizeVerbDestroy} {
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
