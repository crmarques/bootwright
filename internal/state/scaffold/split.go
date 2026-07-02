package scaffold

import (
	"fmt"
	"strings"
)

// splitObjectFiles emits one File per YAML document in body, named
// dir+<metadata.name>.yaml, so scaffolded output keeps one object per file (the
// authoring layout specs/state-model.md defines).
func splitObjectFiles(dir, body string) ([]File, error) {
	docs := splitYAMLDocuments(body)
	files := make([]File, 0, len(docs))
	for _, doc := range docs {
		name := objectMetadataName(doc)
		if name == "" {
			return nil, fmt.Errorf("scaffold object in %s has no metadata.name:\n%s", dir, doc)
		}
		files = append(files, File{Name: dir + name + ".yaml", Body: strings.TrimRight(doc, "\n") + "\n"})
	}
	return files, nil
}

func splitYAMLDocuments(body string) []string {
	var docs []string
	var cur []string
	flush := func() {
		text := strings.Trim(strings.Join(cur, "\n"), "\n")
		if strings.TrimSpace(text) != "" {
			docs = append(docs, text)
		}
		cur = nil
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "---" {
			flush()
			continue
		}
		cur = append(cur, line)
	}
	flush()
	return docs
}

func objectMetadataName(doc string) string {
	inMetadata := false
	for _, line := range strings.Split(doc, "\n") {
		switch {
		case line == "metadata:":
			inMetadata = true
		case inMetadata && strings.HasPrefix(line, "  name:"):
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "name:"))
		case inMetadata && line != "" && !strings.HasPrefix(line, "  "):
			inMetadata = false
		}
	}
	return ""
}
