package ansiblelocal

import (
	"context"
	"io/fs"
	"maps"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// pendingArea is a writable pending bundle holding exactly the given files
// under automation/.
type pendingArea struct {
	prerequisites.BundleArea
	files map[string][]byte
}

func (area pendingArea) Read(_ context.Context, path string, _ int) ([]byte, error) {
	data, ok := area.files[strings.TrimPrefix(path, "automation/")]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return data, nil
}

func (pendingArea) Location(context.Context) (prerequisites.BundleLocation, error) {
	return prerequisites.BundleLocation{Path: "/pending", Device: 1, Inode: 1, Writable: true}, nil
}

// Documentation leaves the automation digest, so the adapter's request compares
// only what the digest covers: an execution area whose CHANGELOG is an older
// one still carries this executable's automation, and one whose role differs
// does not.
func TestTheAdapterRequestComparesOnlyTheDigestedAutomation(t *testing.T) {
	const (
		changelog = "collections/ansible_collections/bootwright/core/CHANGELOG.rst"
		role      = "collections/ansible_collections/bootwright/core/roles/controller_prerequisites/tasks/main.yml"
	)
	if _, ok := ansible.Automation()[role]; !ok {
		t.Fatalf("the fixture role file %s is not embedded automation", role)
	}
	definition := prerequisites.Definition{CatalogDigest: strings.Repeat("a", 64)}
	request := func(files map[string][]byte) error {
		_, err := New(unusedExecution{t}).request(t.Context(), pendingArea{files: files}, nil, "setup", prerequisites.Platform{}, definition, prerequisites.SetupEgress{}, nil)
		return err
	}
	files := maps.Clone(ansible.Assets())
	files[changelog] = []byte("An older release\n")
	if err := request(files); err != nil {
		t.Fatalf("an execution area whose CHANGELOG differs was refused: %v", err)
	}
	files = maps.Clone(ansible.Assets())
	files[role] = append([]byte("# drifted\n"), files[role]...)
	err := request(files)
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "controller.identity" || !strings.Contains(reported[0].Message, "automation") {
		t.Fatalf("an execution area whose role differs = %+v (%v)", reported, err)
	}
}
