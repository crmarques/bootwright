package ceph

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/ansible"
)

type specOption struct {
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
	Elements string `yaml:"elements"`
}

type specEntry struct {
	Options map[string]struct {
		specOption `yaml:",inline"`
		Options    map[string]specOption `yaml:"options"`
	} `yaml:"options"`
}

func ansibleType(t reflect.Type) (string, string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return "str", ""
	case reflect.Int, reflect.Int64:
		return "int", ""
	case reflect.Bool:
		return "bool", ""
	case reflect.Map, reflect.Struct:
		return "dict", ""
	case reflect.Slice:
		if t == reflect.TypeFor[json.RawMessage]() {
			return "dict", ""
		}
		element, _ := ansibleType(t.Elem())
		return "list", element
	}
	return t.Kind().String(), ""
}

func jsonFields(t reflect.Type) map[string]specOption {
	fields := map[string]specOption{}
	for field := range t.Fields() {
		name, flags, _ := strings.Cut(field.Tag.Get("json"), ",")
		kind, elements := ansibleType(field.Type)
		fields[name] = specOption{Type: kind, Required: !strings.Contains(flags, "omitempty"), Elements: elements}
	}
	return fields
}

func TestEveryCephRoleEntryPointDeclaresExactlyTheRequestItReceives(t *testing.T) {
	for _, role := range []struct {
		name, variable string
		entries        []string
		request        reflect.Type
	}{
		{"storage_ceph_core", "bootwright_ceph_request", []string{"apply", "observe", "destroy"}, reflect.TypeFor[Request]()},
		{"storage_ceph_image", "bootwright_ceph_image_request", []string{"resolve", "observe", "destroy"}, reflect.TypeFor[ImageRequest]()},
		{"storage_ceph_host", "bootwright_ceph_host_request", []string{"prepare", "observe", "destroy"}, reflect.TypeFor[HostRequest]()},
	} {
		requireRoleRequest(t, role.name, role.variable, role.entries, role.request)
	}
}

func TestReplicationRoleEntryPointDeclaresExactlyTheRequestItReceives(t *testing.T) {
	requireRoleRequest(t, "storage_ceph_replication", "bootwright_ceph_replication_request", []string{"apply", "observe", "destroy"}, reflect.TypeFor[ReplicationRequest]())
}

func TestSMBRoleEntryPointDeclaresExactlyTheRequestItReceives(t *testing.T) {
	requireRoleRequest(t, "storage_ceph_smb", "bootwright_ceph_smb_request", []string{"apply", "observe", "destroy"}, reflect.TypeFor[SMBRequest]())
}

func requireRoleRequest(t *testing.T, role, variable string, entries []string, request reflect.Type) {
	t.Helper()
	path := "collections/ansible_collections/bootwright/core/roles/" + role + "/meta/argument_specs.yml"
	var document struct {
		ArgumentSpecs map[string]specEntry `yaml:"argument_specs"`
	}
	if err := yaml.Unmarshal(ansible.Assets()[path], &document); err != nil || len(document.ArgumentSpecs) == 0 {
		t.Fatalf("%s: no argument specification (%v)", path, err)
	}
	want := jsonFields(request)
	for _, entry := range entries {
		t.Run(role+"/"+entry, func(t *testing.T) {
			declared, ok := document.ArgumentSpecs[entry].Options[variable]
			if !ok || declared.Type != "dict" || !declared.Required {
				t.Fatalf("%s does not require the dict %s", entry, variable)
			}
			for _, name := range slices.Sorted(maps.Keys(declared.Options)) {
				if _, sent := want[name]; !sent {
					t.Errorf("%s.%s is declared, and the request is never encoded with it", variable, name)
				}
			}
			for _, name := range slices.Sorted(maps.Keys(want)) {
				got, ok := declared.Options[name]
				if !ok {
					t.Errorf("%s refuses %s.%s, which the request is encoded with", entry, variable, name)
					continue
				}
				if got != want[name] {
					t.Errorf("%s.%s is declared %s, the request encodes %s", variable, name, describe(got), describe(want[name]))
				}
			}
		})
	}
}

func describe(o specOption) string {
	return fmt.Sprintf("%s of %q, required %t", o.Type, o.Elements, o.Required)
}
