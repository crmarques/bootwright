package architecture_test

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// secretReferenceField is a backticked API field whose last segment names a
// reference, and secretWord the word that says what it references.
var (
	secretReferenceField = regexp.MustCompile("`[A-Za-z0-9_.\\[\\]]*Refs?`")
	secretWord           = regexp.MustCompile(`\bSecrets?\b`)
	secretClass          = regexp.MustCompile(`(?i)access credentials?|effect material|trust anchors?`)
)

// unclassifiedSecretReferences returns, for one API page, every table row
// whose field is a reference to a Secret and every prose paragraph or list
// item that names one, which states no class of bound Secret: effect
// material, access credential or trust anchor (specs/secrets.md#immutable-binding-and-contexts).
func unclassifiedSecretReferences(name, text string) []string {
	var found []string
	var paragraph []string
	flush := func() {
		joined := strings.Join(paragraph, " ")
		paragraph = nil
		if secretReferenceField.MatchString(joined) && secretWord.MatchString(joined) && !secretClass.MatchString(joined) {
			found = append(found, name+": "+joined)
		}
	}
	fenced := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			flush()
			fenced = !fenced
		case fenced:
		case strings.HasPrefix(trimmed, "|"):
			flush()
			cells := strings.Split(strings.Trim(trimmed, "|"), "|")
			field := strings.TrimSpace(cells[0])
			if secretReferenceField.FindString(field) == field && secretWord.MatchString(trimmed) && !secretClass.MatchString(trimmed) {
				found = append(found, name+": "+trimmed)
			}
		case trimmed == "", strings.HasPrefix(trimmed, "#"), strings.HasPrefix(trimmed, "- "):
			flush()
			if strings.HasPrefix(trimmed, "- ") {
				paragraph = append(paragraph, trimmed)
			}
		default:
			paragraph = append(paragraph, trimmed)
		}
	}
	flush()
	return found
}

// Every API field that references a Secret names the class of bound Secret it
// is, in its table row or in the paragraph that defines it, so the rebind exit
// and every reader can tell an access credential from effect material and from
// a trust anchor.
func TestDocsSecretReferencesNameTheirClass(t *testing.T) {
	root := filepath.Join("..", "..")
	names, err := trackedFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	pages := 0
	for _, name := range names {
		if path.Dir(name) != "specs/api" || !strings.HasSuffix(name, ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, unclassified := range unclassifiedSecretReferences(name, string(data)) {
			t.Errorf("a Secret reference names no class (effect material, access credential or trust anchor): %s", unclassified)
		}
	}
	if pages == 0 {
		t.Fatal("no API page was read")
	}
}

func TestDocsSecretReferenceClassCheckFindsAnUnclassifiedField(t *testing.T) {
	page := strings.Join([]string{
		"| Field | Type | Rule |",
		"| `credentialsRef` | string | `usernamePassword` `Secret`. |",
		"| `classifiedRef` | string | `token` Secret. Access credential. |",
		"| `anchorRef` | string | `caBundle` Secret. Trust anchor. |",
		"| `machineRef` | string | Global Machine. |",
		"",
		"`keyRef` names an `sshKeyPair`",
		"`Secret` that the install places.",
		"",
		"- `otherRef` names a `token` Secret, effect material.",
		"",
		"```yaml",
		"keyRef: names a Secret",
		"```",
	}, "\n")
	found := unclassifiedSecretReferences("page.md", page)
	if len(found) != 2 || !strings.Contains(found[0], "`credentialsRef`") || !strings.Contains(found[1], "`keyRef` names an `sshKeyPair` `Secret` that the install places.") {
		t.Fatalf("unclassified = %q, want the credentialsRef row and the keyRef paragraph", found)
	}
}
