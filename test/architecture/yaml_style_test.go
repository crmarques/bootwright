package architecture_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

var (
	templateAction     = regexp.MustCompile(`\{\{.*?\}\}|\{%.*?%\}`)
	templateActionLine = regexp.MustCompile(`^\s*(?:(?:\{\{.*?\}\}|\{%.*?%\}|\{#.*?#\})\s*)+$`)
	markdownYAMLFence  = regexp.MustCompile("^(\\s*)```ya?ml\\s*$")
	pythonYAMLStrings  = []*regexp.Regexp{
		regexp.MustCompile(`(?ms)^(?:DOCUMENTATION|EXAMPLES|RETURN) = r?"""\n(.*?)\n"""`),
		regexp.MustCompile(`(?ms)^(?:DOCUMENTATION|EXAMPLES|RETURN) = r?'''\n(.*?)\n'''`),
	}
)

// YAML is written in block style: a non-empty mapping or sequence never uses
// flow syntax. Only an empty `{}` or `[]`, which block style cannot spell,
// stays flow. The rule covers authored YAML, YAML a template renders (with its
// actions neutralized), YAML fenced in Markdown, the YAML strings of Ansible
// modules, and the YAML documents the encoder writes into goldens.
func TestYAMLUsesBlockStyle(t *testing.T) {
	root := filepath.Join("..", "..")
	var violations []string
	for _, name := range repositoryFiles(t) {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, source := range yamlSources(name, string(data)) {
			found, err := flowCollections(source.text)
			if err != nil {
				violations = append(violations, fmt.Sprintf("%s:%d: does not parse as YAML: %v", name, source.line, err))
				continue
			}
			for _, line := range found {
				violations = append(violations, fmt.Sprintf("%s:%d: a non-empty flow collection; write it in block style", name, source.line+line-1))
			}
		}
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}

func TestFlowCollectionsFindsOnlyNonEmptyFlow(t *testing.T) {
	source := "a: [1, 2]\nb: []\nc: {}\nd:\n  - {x: 1}\ne:\n  - f\n"
	found, err := flowCollections(source)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 5}; fmt.Sprint(found) != fmt.Sprint(want) {
		t.Fatalf("flow collections at %v, want %v", found, want)
	}
	if found := yamlSources("t/templates/x.yaml", "{{- if .a }}\nb: {{ .c }}\n{{- end }}\n"); len(found) != 1 || found[0].text != "\nb: TPL\n\n" {
		t.Fatalf("template neutralized as %+v", found)
	}
}

type yamlSource struct {
	line int
	text string
}

func yamlSources(name, data string) []yamlSource {
	switch {
	case strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml"):
		return []yamlSource{{line: 1, text: neutralizeTemplates(data)}}
	case strings.HasSuffix(name, ".golden") && strings.HasPrefix(data, "apiVersion:"):
		return []yamlSource{{line: 1, text: data}}
	case strings.HasSuffix(name, ".md"):
		return markdownYAML(data)
	case strings.HasSuffix(name, ".py") && strings.Contains(name, "/plugins/"):
		return pythonYAML(data)
	}
	return nil
}

func pythonYAML(data string) []yamlSource {
	var sources []yamlSource
	for _, pattern := range pythonYAMLStrings {
		for _, match := range pattern.FindAllStringSubmatchIndex(data, -1) {
			line := strings.Count(data[:match[2]], "\n") + 1
			sources = append(sources, yamlSource{line: line, text: neutralizeTemplates(data[match[2]:match[3]])})
		}
	}
	return sources
}

func neutralizeTemplates(data string) string {
	if !strings.Contains(data, "{{") && !strings.Contains(data, "{%") {
		return data
	}
	lines := strings.Split(data, "\n")
	for i, line := range lines {
		if templateActionLine.MatchString(line) {
			lines[i] = ""
			continue
		}
		lines[i] = templateAction.ReplaceAllString(line, "TPL")
	}
	return strings.Join(lines, "\n")
}

func markdownYAML(data string) []yamlSource {
	lines := strings.Split(data, "\n")
	var sources []yamlSource
	for i := 0; i < len(lines); i++ {
		match := markdownYAMLFence.FindStringSubmatch(lines[i])
		if match == nil {
			continue
		}
		start := i + 1
		var body []string
		for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
			body = append(body, strings.TrimPrefix(lines[i], match[1]))
		}
		sources = append(sources, yamlSource{line: start + 1, text: neutralizeTemplates(strings.Join(body, "\n"))})
	}
	return sources
}

func flowCollections(text string) ([]int, error) {
	decoder := yaml.NewDecoder(bytes.NewReader([]byte(text)))
	var lines []int
	for {
		var document yaml.Node
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			return lines, nil
		}
		if err != nil {
			return nil, err
		}
		lines = appendFlowLines(lines, &document)
	}
}

func appendFlowLines(lines []int, node *yaml.Node) []int {
	if (node.Kind == yaml.MappingNode || node.Kind == yaml.SequenceNode) && node.Style&yaml.FlowStyle != 0 && len(node.Content) > 0 {
		lines = append(lines, node.Line)
	}
	for _, child := range node.Content {
		lines = appendFlowLines(lines, child)
	}
	return lines
}
