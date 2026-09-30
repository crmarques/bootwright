//go:build linux && amd64

package localkeyring

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type tornArea struct {
	secretstore.Area
	root     string
	tearAt   int
	shape    func([]byte) []byte
	tearKey  bool
	replaces int
	fired    bool
	target   string
}

func (a *tornArea) Replace(ctx context.Context, path string, data, expected []byte) (secretstore.Outcome, error) {
	a.replaces++
	if a.replaces != a.tearAt {
		return a.Area.Replace(ctx, path, data, expected)
	}
	directory := filepath.Join(a.root, filepath.Dir(path))
	if err := os.MkdirAll(directory, 0700); err != nil {
		return secretstore.NotCommitted, err
	}
	stage := filepath.Join(directory, fmt.Sprintf("pending-%032x", a.replaces))
	if err := os.WriteFile(stage, a.shape(data), 0600); err != nil {
		return secretstore.NotCommitted, err
	}
	a.fired, a.target = true, path
	return secretstore.NotCommitted, errSimulatedCrash
}

func (a *tornArea) WriteExclusive(ctx context.Context, path string, data []byte) error {
	if !a.tearKey || !strings.HasSuffix(path, ".key") {
		return a.Area.WriteExclusive(ctx, path, data)
	}
	a.tearKey = false
	if err := a.Area.WriteExclusive(ctx, path, data[:16]); err != nil {
		return err
	}
	a.fired = true
	return errSimulatedCrash
}

type tornSite struct {
	name    string
	target  string
	replace int
	preface func(*testing.T, *integrationStore)
}

func tornSites() []tornSite {
	return []tornSite{
		{name: "fresh-marker", target: initializationPath, replace: 1},
		{name: "signed-marker", target: initializationPath, replace: 2},
		{name: "metadata", target: selectorPath, replace: 3},
		{name: "ledger", target: ".usage.json", replace: 1, preface: func(t *testing.T, h *integrationStore) {
			t.Helper()
			crash := &crashArea{failBeforeIndex: true}
			if err := h.initializeThrough(func(area secretstore.Area) secretstore.Area { crash.Area = area; return crash }); err == nil || crash.failBeforeIndex {
				t.Fatalf("the metadata interruption did not fire: %v", err)
			}
		}},
		{name: "next-marker", target: initializationPath, replace: 1, preface: func(t *testing.T, h *integrationStore) {
			t.Helper()
			torn := &tornArea{tearKey: true}
			if err := h.initializeThrough(func(area secretstore.Area) secretstore.Area { torn.Area = area; return torn }); err == nil || !torn.fired {
				t.Fatalf("the key interruption did not fire: %v", err)
			}
		}},
	}
}

func (h *integrationStore) secretsRoot() string {
	return filepath.Join(h.root, "contexts", h.context.Name, "secrets")
}

func (h *integrationStore) initializeThrough(wrap func(secretstore.Area) secretstore.Area) error {
	return h.workspace.MutateSecrets(context.Background(), h.context, func(area secretstore.Area) error {
		session, err := h.implementation.Initialize(context.Background(), h.context, wrap(area), nil)
		if session != nil {
			session.Close()
		}
		return err
	})
}

func (h *integrationStore) tear(t *testing.T, site tornSite, shape func([]byte) []byte) {
	t.Helper()
	if site.preface != nil {
		site.preface(t, h)
	}
	torn := &tornArea{root: h.secretsRoot(), tearAt: site.replace, shape: shape}
	err := h.initializeThrough(func(area secretstore.Area) secretstore.Area { torn.Area = area; return torn })
	if err == nil || !torn.fired || !strings.HasSuffix(torn.target, site.target) {
		t.Fatalf("the %s stage was not torn: target=%q err=%v", site.name, torn.target, err)
	}
}

func pendingStages(t *testing.T, root string) []string {
	t.Helper()
	var stages []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(entry.Name(), "pending-") {
			stages = append(stages, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return stages
}

func TestAnInitializationResumesOverATornStageAtEveryStagedWrite(t *testing.T) {
	shapes := []struct {
		name  string
		shape func([]byte) []byte
	}{
		{name: "empty", shape: func([]byte) []byte { return nil }},
		{name: "half", shape: func(data []byte) []byte { return data[:len(data)/2] }},
	}
	for _, site := range tornSites() {
		for _, shape := range shapes {
			t.Run(site.name+"/"+shape.name, func(t *testing.T) {
				h := newUninitializedIntegrationStore(t)
				h.tear(t, site, shape.shape)
				err := h.workspace.MutateSecrets(context.Background(), h.context, func(area secretstore.Area) error {
					session, err := h.implementation.Initialize(context.Background(), h.context, area, nil)
					if err != nil {
						return err
					}
					defer session.Close()
					snapshot, err := session.Inspect(context.Background())
					if err != nil || len(snapshot.Keys) != 1 || snapshot.ActiveKey == "" {
						t.Fatalf("resumed initialization: %#v %v", snapshot, err)
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				if stages := pendingStages(t, h.secretsRoot()); len(stages) != 0 {
					t.Fatalf("stages remain after the resumed initialization: %v", stages)
				}
			})
		}
	}
}

func TestAnInitializationRefusesAStageThatParsesButAttributesNothing(t *testing.T) {
	shapes := []struct {
		name  string
		shape func([]byte) []byte
	}{
		{name: "unterminated", shape: func(data []byte) []byte { return data[:len(data)-1] }},
		{name: "foreign", shape: func([]byte) []byte { return []byte("{\"formatVersion\":3}\n") }},
	}
	for _, site := range tornSites() {
		for _, shape := range shapes {
			t.Run(site.name+"/"+shape.name, func(t *testing.T) {
				h := newUninitializedIntegrationStore(t)
				h.tear(t, site, shape.shape)
				guard := &rejectMutationArea{}
				err := h.initializeThrough(func(area secretstore.Area) secretstore.Area { guard.Area = area; return guard })
				if failureCode(err) != "secret.store.corrupt" || guard.effects != 0 {
					t.Fatalf("unattributable stage: code=%q mutation-attempts=%d error=%v", failureCode(err), guard.effects, err)
				}
			})
		}
	}
}
