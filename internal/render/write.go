package render

import (
	"fmt"

	"go.yaml.in/yaml/v3"
)

func writeText(fs FileSystem, path string, content string) error {
	if err := fs.WriteAtomic(path, []byte(content), localFileMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func writeYAML(fs FileSystem, path string, value any) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	if err := fs.WriteAtomic(path, data, localFileMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func writeYAMLDocuments(fs FileSystem, path string, values []any) error {
	var data []byte
	for i, value := range values {
		chunk, err := yaml.Marshal(value)
		if err != nil {
			return fmt.Errorf("marshal %s: %w", path, err)
		}
		if i > 0 {
			data = append(data, []byte("---\n")...)
		}
		data = append(data, chunk...)
	}
	if err := fs.WriteAtomic(path, data, localFileMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
