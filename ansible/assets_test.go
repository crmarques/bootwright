package ansible

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
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
	files := Assets()
	automation, documentation := split(files)
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
// CHANGELOG keeps the digest and with it the approved bundle.
func TestDocumentationLeavesTheAutomationDigest(t *testing.T) {
	_, documentation := split(Assets())
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
}

// A role states the frozen request version it accepts twice: each entry point
// of its argument spec admits one, and its first task asserts it. When a request
// version moves and only one of them follows, every call through that role fails
// on the stale assertion before the adapter reports anything, so the two are
// kept in step here rather than on a host. The managed service role's artifact
// server entry points admit the artifact server's own version, which its shared
// tasks assert beside the network services' one. The substrate's machine port dispatcher takes no
// frozen request of its own, so it states no version. The shared pinned-SSH
// identity helper likewise consumes its caller's validated identity port, and
// the shared artifact listener proof its caller's URL.
func TestRoleVersionAssertionsMatchTheirArgumentSpecs(t *testing.T) {
	admitted := map[string][]string{}
	asserted := map[string][]string{}
	for name, data := range Assets() {
		role, ok := roleOwning(name)
		if !ok || role == "substrate_machine" || role == "machine_ssh_identity" || role == "infrastructureservices_artifact_listener" {
			continue
		}
		switch {
		case strings.HasSuffix(name, "/meta/argument_specs.yml"):
			choices, each := admittedVersions(string(data))
			if !each || len(choices) == 0 {
				t.Fatalf("%s admits %d request versions; each entry point accepts exactly one", name, len(choices))
			}
			admitted[role] = choices
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
			if !slices.Contains(expected, version) {
				t.Fatalf("role %s asserts request version %q but its argument spec admits %q", role, version, expected)
			}
		}
	}
	for role, versions := range admitted {
		for _, version := range versions {
			if !slices.Contains(asserted[role], version) {
				t.Fatalf("role %s admits request version %q, which no task asserts", role, version)
			}
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

func admittedVersions(specification string) ([]string, bool) {
	var document struct {
		ArgumentSpecs yaml.Node `yaml:"argument_specs"`
	}
	if err := yaml.Unmarshal([]byte(specification), &document); err != nil || document.ArgumentSpecs.Kind != yaml.MappingNode {
		return nil, false
	}
	var found []string
	for index := 1; index < len(document.ArgumentSpecs.Content); index += 2 {
		entry := versionChoices(document.ArgumentSpecs.Content[index], nil)
		slices.Sort(entry)
		if entry = slices.Compact(entry); len(entry) > 1 {
			return nil, false
		}
		found = append(found, entry...)
	}
	slices.Sort(found)
	return slices.Compact(found), true
}

func versionChoices(node *yaml.Node, found []string) []string {
	if node.Kind == yaml.AliasNode && node.Alias != nil {
		node = node.Alias
	}
	if node.Kind == yaml.MappingNode {
		for index := 0; index+1 < len(node.Content); index += 2 {
			key, value := node.Content[index], node.Content[index+1]
			if key.Value == "version" && value.Kind == yaml.MappingNode {
				for option := 0; option+1 < len(value.Content); option += 2 {
					if value.Content[option].Value == "choices" {
						for _, choice := range value.Content[option+1].Content {
							found = append(found, choice.Value)
						}
					}
				}
			}
		}
	}
	for _, child := range node.Content {
		found = versionChoices(child, found)
	}
	return found
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
