package ansible

import (
	"crypto/sha256"
	"encoding/hex"
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
// exactly the files Automation returns against that bundle. It covers every
// one of them, so none can differ under an equal digest. Documentation is the
// only embedded content outside it, and it stays embedded.
func TestAutomationDigestCoversEveryEmbeddedFile(t *testing.T) {
	files, automation, documentation := Assets(), Automation(), Documentation()
	baseline := Digest()
	if digestOf(automation) != baseline {
		t.Fatal("the automation digest does not cover exactly the automation files")
	}
	if len(automation)+len(documentation) != len(files) {
		t.Fatalf("automation (%d) and documentation (%d) do not partition the %d embedded files", len(automation), len(documentation), len(files))
	}
	for name, data := range files {
		kept, inAutomation := automation[name]
		described, inDocumentation := documentation[name]
		if inAutomation == inDocumentation {
			t.Fatalf("%s is in automation %v and in documentation %v; it belongs to exactly one", name, inAutomation, inDocumentation)
		}
		if !slices.Equal(kept, data) && !slices.Equal(described, data) {
			t.Fatalf("%s is not the embedded file", name)
		}
	}
	for _, name := range []string{
		"collections/ansible_collections/bootwright/core/README.md",
		"collections/ansible_collections/bootwright/core/CHANGELOG.rst",
	} {
		if _, ok := files[name]; !ok {
			t.Fatalf("the embedded collection no longer carries %s", name)
		}
	}
	for name, data := range automation {
		changed := maps.Clone(automation)
		changed[name] = append(slices.Clone(data), '\n')
		if digestOf(changed) == baseline {
			t.Fatalf("changing %s leaves the automation digest unchanged", name)
		}
	}
}

// Documentation runs nothing, so a build that changes only the README or the
// CHANGELOG keeps the digest and with it the approved bundle. The narrowed
// digest has its own domain version: no digest that covered documentation can
// equal it.
func TestDocumentationLeavesTheAutomationDigest(t *testing.T) {
	documentation := Documentation()
	want := []string{
		"collections/ansible_collections/bootwright/core/CHANGELOG.rst",
		"collections/ansible_collections/bootwright/core/README.md",
	}
	if got := slices.Sorted(maps.Keys(documentation)); !slices.Equal(got, want) {
		t.Fatalf("documentation = %v; want exactly %v", got, want)
	}
	baseline := Digest()
	for _, name := range want {
		changed := maps.Clone(Assets())
		changed[name] = append(slices.Clone(changed[name]), "\nA later release.\n"...)
		automation, _ := split(changed)
		if digestOf(automation) != baseline {
			t.Fatalf("changing %s moved the automation digest", name)
		}
	}
	if digestUnderV1(Automation()) == baseline {
		t.Fatal("the narrowed digest kept the domain version of the digest that covered documentation")
	}
}

// digestUnderV1 is the digest as it was computed while it covered
// documentation, kept here only to prove the two domains never collide.
func digestUnderV1(files map[string][]byte) string {
	digest := sha256.New()
	digest.Write([]byte("bootwright.controller.automation-v1\x00"))
	for _, name := range slices.Sorted(maps.Keys(files)) {
		digest.Write([]byte(name))
		digest.Write([]byte{0})
		content := sha256.Sum256(files[name])
		digest.Write(content[:])
	}
	return hex.EncodeToString(digest.Sum(nil))
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
