package networking

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
)

func TestNetworkAdapterFixtureMatchesRenderedProgram(t *testing.T) {
	program, err := Render(clusterFixture(), noMachines)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := co.Canonical(program)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "..", "..", "ansible", "collections", "ansible_collections", "bootwright", "core", "tests", "unit", "fixtures", "cluster_objects", "network.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	actual, err := co.Canonical(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(expected) {
		t.Fatal("network adapter fixture differs from rendered program")
	}
}
