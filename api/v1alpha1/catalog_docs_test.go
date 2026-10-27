package v1alpha1

import (
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var catalogPageLink = regexp.MustCompile(`\]\((api/[a-z-]+\.md)#kinds\)`)

func specSection(t *testing.T, file, heading, next string) string {
	t.Helper()
	data, err := os.ReadFile(path.Join("../../specs", file))
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(data), "\n"+heading+"\n")
	if !ok {
		t.Fatalf("specs/%s has no %q section", file, heading)
	}
	section, _, _ = strings.Cut(section, next)
	return section
}

// catalogPages reads api.md's Kind catalog: each listed kind and the field
// page its row links.
func catalogPages(t *testing.T) map[Kind]string {
	t.Helper()
	pages := map[Kind]string{}
	for _, line := range strings.Split(specSection(t, "api.md", "## Kind catalog", "\n### "), "\n") {
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| ---") || len(cells) < 4 || strings.TrimSpace(cells[1]) == "Kinds" {
			continue
		}
		link := catalogPageLink.FindStringSubmatch(cells[3])
		for _, name := range backticked.FindAllStringSubmatch(cells[1], -1) {
			if _, repeated := pages[Kind(name[1])]; repeated {
				t.Errorf("api.md's Kind catalog lists %s twice", name[1])
			}
			if link == nil {
				t.Errorf("api.md's Kind catalog row of %s links no field page Kinds table", name[1])
				continue
			}
			pages[Kind(name[1])] = link[1]
		}
	}
	return pages
}

func pageKinds(t *testing.T, file string) []Kind {
	t.Helper()
	var listed []Kind
	for _, line := range strings.Split(specSection(t, file, "## Kinds", "\n## "), "\n") {
		if name, found := strings.CutPrefix(line, "| `"); found {
			name, _, _ = strings.Cut(name, "`")
			listed = append(listed, Kind(name))
		}
	}
	return listed
}

// F02-04: every registered kind has its api.md Kind catalog row, its place in
// the canonical kind order of effective output, and a row in the Kinds table
// of the field page its catalog row links; no page lists a kind another page
// owns or the registry lacks.
func TestEveryKindHasItsCatalogRowOrderAndPageRow(t *testing.T) {
	pages := catalogPages(t)
	catalogued := make([]Kind, 0, len(pages))
	for kind := range pages {
		catalogued = append(catalogued, kind)
	}
	registered := Kinds()
	slices.Sort(catalogued)
	sortedRegistered := slices.Clone(registered)
	slices.Sort(sortedRegistered)
	if !slices.Equal(catalogued, sortedRegistered) {
		t.Errorf("api.md's Kind catalog lists %v; the registry holds %v", catalogued, sortedRegistered)
	}
	order := specSection(t, "api.md", "Canonical effective output:", "\n2. ")
	var ordered []Kind
	for _, name := range backticked.FindAllStringSubmatch(order, -1) {
		ordered = append(ordered, Kind(name[1]))
	}
	if !slices.Equal(ordered, registered) {
		t.Errorf("api.md's canonical kind order is %v; the registry orders %v", ordered, registered)
	}
	byPage := map[string][]Kind{}
	for kind, page := range pages {
		byPage[page] = append(byPage[page], kind)
	}
	for page, kinds := range byPage {
		listed := pageKinds(t, page)
		slices.Sort(listed)
		slices.Sort(kinds)
		if !slices.Equal(listed, kinds) {
			t.Errorf("specs/%s Kinds table lists %v; api.md's Kind catalog sends it %v", page, listed, kinds)
		}
	}
}
