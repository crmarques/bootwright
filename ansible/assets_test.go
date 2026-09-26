package ansible

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// An empty value is not an empty list. ansible-core parses a bare "key =" as
// one empty element, so "callbacks_enabled =" loads a callback plugin whose
// name is "", and every playbook aborts before its first task. Omit the key to
// mean none.
func TestConfigurationDeclaresNoEmptyValue(t *testing.T) {
	data, ok := Assets()["ansible.cfg"]
	if !ok {
		t.Fatal("the embedded automation carries no ansible.cfg")
	}
	for number, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "[") {
			continue
		}
		key, value, found := strings.Cut(trimmed, "=")
		if !found {
			t.Fatalf("ansible.cfg line %d is neither a section nor an assignment: %q", number+1, trimmed)
		}
		if strings.TrimSpace(value) == "" {
			t.Fatalf("ansible.cfg line %d assigns %q an empty value; omit the key instead", number+1, strings.TrimSpace(key))
		}
	}
}

// The automation digest names the approved bundle's automation, and the
// lifecycle runner, the controller adapter and bundle inspection each compare
// every embedded file against that bundle. A file the digest skipped could
// differ under an equal digest: setup would then refuse the retained bundle as
// unattributable instead of carrying it forward. Documentation leaves the
// digest (backlog Z2) only together with those comparisons.
func TestAutomationDigestCoversEveryEmbeddedFile(t *testing.T) {
	files := Assets()
	baseline := Digest()
	if digestOf(files) != baseline {
		t.Fatal("the automation digest does not cover exactly the embedded files")
	}
	for _, name := range []string{
		"collections/ansible_collections/bootwright/core/README.md",
		"collections/ansible_collections/bootwright/core/CHANGELOG.rst",
	} {
		if _, ok := files[name]; !ok {
			t.Fatalf("the embedded automation no longer carries %s", name)
		}
	}
	for name, data := range files {
		changed := maps.Clone(files)
		changed[name] = append(slices.Clone(data), '\n')
		if digestOf(changed) == baseline {
			t.Fatalf("changing %s leaves the automation digest unchanged", name)
		}
	}
}

// A role states the frozen request version it accepts twice: its argument spec
// admits one, and its first task asserts one. When a request version moves and
// only one of them follows, every call through that role fails on the stale
// assertion before the adapter reports anything, so the two are kept in step
// here rather than on a host.
func TestRoleVersionAssertionsMatchTheirArgumentSpecs(t *testing.T) {
	admitted := map[string]string{}
	asserted := map[string][]string{}
	for name, data := range Assets() {
		role, ok := roleOwning(name)
		if !ok {
			continue
		}
		switch {
		case strings.HasSuffix(name, "/meta/argument_specs.yml"):
			choices := admittedVersions(string(data))
			if len(choices) != 1 {
				t.Fatalf("%s admits %d request versions; a role accepts exactly one", name, len(choices))
			}
			admitted[role] = choices[0]
		case strings.Contains(name, "/tasks/") && strings.HasSuffix(name, ".yml"):
			asserted[role] = append(asserted[role], assertedVersions(string(data))...)
		}
	}
	for role, versions := range asserted {
		expected, ok := admitted[role]
		if !ok {
			t.Fatalf("role %s asserts a request version its argument spec does not admit", role)
		}
		for _, version := range versions {
			if version != expected {
				t.Fatalf("role %s asserts request version %q but its argument spec admits %q", role, version, expected)
			}
		}
	}
	for role := range admitted {
		if len(asserted[role]) == 0 {
			t.Fatalf("role %s admits a request version no task asserts", role)
		}
	}
}

func roleOwning(name string) (string, bool) {
	rest, ok := strings.CutPrefix(name, "collections/ansible_collections/bootwright/core/roles/")
	if !ok {
		return "", false
	}
	role, _, ok := strings.Cut(rest, "/")
	return role, ok
}

func admittedVersions(specification string) []string {
	var found []string
	lines := strings.Split(specification, "\n")
	for index, line := range lines {
		if strings.TrimSpace(line) != "version:" {
			continue
		}
		option := len(line) - len(strings.TrimLeft(line, " "))
		for _, next := range lines[index+1:] {
			trimmed := strings.TrimSpace(next)
			if trimmed == "" {
				continue
			}
			if len(next)-len(strings.TrimLeft(next, " ")) <= option {
				break
			}
			if choices, ok := strings.CutPrefix(trimmed, "choices: ["); ok {
				for _, choice := range strings.Split(strings.TrimSuffix(choices, "]"), ",") {
					found = append(found, strings.TrimSpace(choice))
				}
			}
		}
	}
	slices.Sort(found)
	return slices.Compact(found)
}

func assertedVersions(tasks string) []string {
	var found []string
	for remainder := tasks; ; {
		_, after, ok := strings.Cut(remainder, ".version == '")
		if !ok {
			return found
		}
		version, rest, ok := strings.Cut(after, "'")
		if !ok {
			return found
		}
		found = append(found, version)
		remainder = rest
	}
}
