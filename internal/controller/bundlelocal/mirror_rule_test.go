package bundlelocal

import (
	"context"
	"errors"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

var refusedByAdmissionAndEveryStage = []string{
	"http://mirror.example.test/helm",
	"https://mirror.example.test:8443/helm",
	"https://mirror.example.test/helm?q",
	"https://mirror.example.test/helm#f",
}

var earlierStageOnlyMirrors = []string{
	"https://mirror.example.test/helm?",
	"https://mirror.example.test/helm#",
	"https://mirror.example.test/helm?#",
}

func mirrorCandidates() []string {
	candidates := []string{
		"https://mirror.example.test/helm",
		"https://mirror.example.test:443/helm",
		"https://mirror.example.test/helm/",
		"https://192.0.2.1/tools",
		"https://Mirror.example.test/helm",
		"https://user@mirror.example.test/helm",
		"https://mirror.example.test/a%2Fb",
	}
	candidates = append(candidates, earlierStageOnlyMirrors...)
	return append(candidates, refusedByAdmissionAndEveryStage...)
}

// Admission and the resolution that acts on a mirror read one predicate, so
// validate refuses every mirror the stage would refuse after registration, and
// the stage refuses it before it contacts any publisher.
func TestTheStageAndAdmissionShareTheMirrorRule(t *testing.T) {
	reached := errors.New("publisher contacted")
	for _, mirror := range mirrorCandidates() {
		calls := 0
		catalog := &ToolCatalog{metadata: func(context.Context, string, string, int64, prerequisites.SetupEgress) (toolMetadata, error) {
			calls++
			return toolMetadata{}, reached
		}}
		_, err := catalog.Resolve(context.Background(), []controller.ToolRequest{{Kind: "helm", Version: "latest", Mirror: mirror}}, prerequisites.SetupEgress{})
		staged := errors.Is(err, reached)
		if admitted := api.ValidLexical("mirror-url", mirror); admitted != staged {
			t.Errorf("mirror %q: admission admits it %t, the stage resolves it %t (%v)", mirror, admitted, staged, err)
		}
		if !staged && calls != 0 {
			t.Errorf("mirror %q: the stage refused it after %d publisher reads", mirror, calls)
		}
	}
	for _, mirror := range append(append([]string{}, refusedByAdmissionAndEveryStage...), earlierStageOnlyMirrors...) {
		if api.ValidLexical("mirror-url", mirror) {
			t.Errorf("admission admitted %q, which the stage refuses", mirror)
		}
	}
}

// Recovery reads a frozen request, which an earlier build may have admitted,
// at that build's stage tolerance as well as admission's rule, so a block
// frozen with a mirror ending in a bare '?' or '#' still reports its closure
// instead of refusing.
func TestRecoveryReadsAFrozenMirrorAtTheEarlierStagesTolerance(t *testing.T) {
	for _, mirror := range mirrorCandidates() {
		_, complete, err := (&ToolCatalog{}).Select([]controller.ToolRequest{{Kind: "helm", Version: "latest", Mirror: mirror}}, nil)
		if tolerated := api.ValidLexical("mirror-url", mirror) || safeToolURL(mirror); tolerated != (err == nil) {
			t.Errorf("mirror %q: the earlier stage tolerated it %t, recovery reads it %t (%v)", mirror, tolerated, err == nil, err)
		}
		if err == nil && complete {
			t.Errorf("mirror %q: no retained source, yet the closure is complete", mirror)
		}
	}
	for _, mirror := range earlierStageOnlyMirrors {
		if _, _, err := (&ToolCatalog{}).Select([]controller.ToolRequest{{Kind: "helm", Version: "latest", Mirror: mirror}}, nil); err != nil {
			t.Errorf("recovery refused %q, which an earlier build froze: %v", mirror, err)
		}
	}
	for _, mirror := range refusedByAdmissionAndEveryStage {
		if _, _, err := (&ToolCatalog{}).Select([]controller.ToolRequest{{Kind: "helm", Version: "latest", Mirror: mirror}}, nil); err == nil {
			t.Errorf("recovery read %q, which no stage ever admitted", mirror)
		}
	}
}
