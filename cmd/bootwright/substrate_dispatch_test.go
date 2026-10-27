package main

import (
	"maps"
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/substrate"
)

// The arms Go realizes and freezes into requests are exactly the arms the
// collection's one machine port dispatcher binds, so an arm reaches every
// consumer through that table alone and none is frozen that no port answers.
func TestTheMachinePortDispatcherBindsExactlyTheRealizedArms(t *testing.T) {
	var defaults struct {
		Arms map[string]string `yaml:"substrate_machine_arms"`
	}
	if err := yaml.Unmarshal(ansible.Assets()[observeRoleRoot+"substrate_machine/defaults/main.yml"], &defaults); err != nil {
		t.Fatal(err)
	}
	if got, want := slices.Sorted(maps.Keys(defaults.Arms)), slices.Sorted(slices.Values(substrate.Realized())); !slices.Equal(got, want) {
		t.Fatalf("the dispatcher binds %q, want the realized arms %q", got, want)
	}
}
