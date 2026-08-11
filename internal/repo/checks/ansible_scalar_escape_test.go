package repocheck

import (
	"path/filepath"
	"regexp"
	"testing"

	"go.yaml.in/yaml/v3"
)

var ansibleJinjaNewlineEscapes = []*regexp.Regexp{
	regexp.MustCompile(`~\s*['"][^'"]*\\n[^'"]*['"]`),
	regexp.MustCompile(`\bjoin\(\s*['"][^'"]*\\n[^'"]*['"]\s*\)`),
}

func TestAnsibleJinjaNewlineEscapesAreYAMLDecoded(t *testing.T) {
	root := filepath.Join(repoRoot(t), "ansible", "collections", "ansible_collections", "bootwright", "core")
	walkSourceFiles(t, root, ".yml", func(path string, src []byte) {
		var document yaml.Node
		if err := yaml.Unmarshal(src, &document); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		rel, err := filepath.Rel(repoRoot(t), path)
		if err != nil {
			t.Fatalf("resolve %s: %v", path, err)
		}
		walkAnsibleScalarNodes(&document, func(node *yaml.Node) {
			if hasUndecodedAnsibleJinjaNewlineEscape(node.Value) {
				t.Errorf("%s:%d contains an undecoded Jinja newline escape; YAML must decode the escape before Jinja evaluates it", filepath.ToSlash(rel), node.Line)
			}
		})
	})
}

func TestUndecodedAnsibleJinjaNewlineEscapeDetection(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{name: "double quoted concatenation", source: `value: "{{ item ~ '\n' }}"`, want: false},
		{name: "folded concatenation", source: "value: >-\n  {{ item ~ '\\n' }}\n", want: true},
		{name: "double escaped concatenation", source: `value: "{{ item ~ '\\n' }}"`, want: true},
		{name: "double quoted prefixed join", source: `value: "{{ items | join('---\n') }}"`, want: false},
		{name: "folded prefixed join", source: "value: >-\n  {{ items | join('---\\n') }}\n", want: true},
		{name: "unrelated escape", source: `value: "printf '\\n'"`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var document yaml.Node
			if err := yaml.Unmarshal([]byte(tt.source), &document); err != nil {
				t.Fatalf("decode fixture: %v", err)
			}
			got := false
			walkAnsibleScalarNodes(&document, func(node *yaml.Node) {
				got = got || hasUndecodedAnsibleJinjaNewlineEscape(node.Value)
			})
			if got != tt.want {
				t.Fatalf("detected=%t, want %t", got, tt.want)
			}
		})
	}
}

func hasUndecodedAnsibleJinjaNewlineEscape(value string) bool {
	for _, pattern := range ansibleJinjaNewlineEscapes {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func walkAnsibleScalarNodes(node *yaml.Node, visit func(*yaml.Node)) {
	if node == nil {
		return
	}
	if node.Kind == yaml.ScalarNode {
		visit(node)
	}
	for _, child := range node.Content {
		walkAnsibleScalarNodes(child, visit)
	}
}
