package architecture_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var bmcSenseController = regexp.MustCompile(`(?i)\b(declares no controller|controller power|the controller accepts)\b`)

var bmcControllerPages = []string{
	"specs/substrates.md", "specs/api/machines.md", "specs/managed-os.md", "specs/state-reconciliation.md",
}

func bmcSenseControllers(name, text string) []string {
	var found []string
	fenced := false
	for number, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
		}
		if !fenced && bmcSenseController.MatchString(line) {
			found = append(found, name+":"+strconv.Itoa(number+1)+": "+strings.TrimSpace(line))
		}
	}
	return found
}

// The glossary reserves bare "controller" for the Bootwright host, so a page
// that means a Machine's BMC says "BMC" or "management controller".
func TestDocsBMCIsNeverCalledTheController(t *testing.T) {
	for _, name := range bmcControllerPages {
		data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		for _, found := range bmcSenseControllers(name, string(data)) {
			t.Errorf("bare \"controller\" names a BMC; say \"BMC\" or \"management controller\" (specs/glossary.md): %s", found)
		}
	}
}

func TestDocsBMCControllerCheckFindsEachPhrasing(t *testing.T) {
	page := strings.Join([]string{
		"A Machine that declares no controller, MAC or hint.",
		"| Machine | domain state; controller power |",
		"a type the controller accepts",
		"the controller-to-BMC leg, the controller host and the management controller are fine.",
		"```",
		"controller power",
		"```",
	}, "\n")
	if found := bmcSenseControllers("page.md", page); len(found) != 3 {
		t.Fatalf("found = %q, want the three BMC-sense lines outside the fence", found)
	}
}
