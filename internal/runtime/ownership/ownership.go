package ownership

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	safefs "github.com/crmarques/bootwright/internal/runtime/fs"
)

const ResourceDirName = "resources"

type ResourceRecord struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	Owner      string            `json:"owner"`
	Context    string            `json:"context,omitempty"`
	Host       string            `json:"host,omitempty"`
	Provider   string            `json:"provider,omitempty"`
	Cluster    string            `json:"cluster,omitempty"`
	Machine    string            `json:"machine,omitempty"`
	Paths      []string          `json:"paths,omitempty"`
	HostFacts  map[string]string `json:"hostFacts,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

var safeSegmentRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func (record *ResourceRecord) UnmarshalJSON(data []byte) error {
	var wire struct {
		APIVersion string            `json:"apiVersion"`
		Kind       string            `json:"kind"`
		Name       string            `json:"name"`
		Owner      string            `json:"owner"`
		Context    string            `json:"context,omitempty"`
		Host       string            `json:"host,omitempty"`
		Provider   string            `json:"provider,omitempty"`
		Cluster    string            `json:"cluster,omitempty"`
		Machine    string            `json:"machine,omitempty"`
		Paths      []string          `json:"paths,omitempty"`
		HostFacts  map[string]string `json:"hostFacts,omitempty"`
		Labels     map[string]string `json:"labels,omitempty"`
		Attributes map[string]string `json:"attributes,omitempty"`
		UpdatedAt  string            `json:"updatedAt"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*record = ResourceRecord{
		APIVersion: wire.APIVersion,
		Kind:       wire.Kind,
		Name:       wire.Name,
		Owner:      wire.Owner,
		Context:    wire.Context,
		Host:       wire.Host,
		Provider:   wire.Provider,
		Cluster:    wire.Cluster,
		Machine:    wire.Machine,
		Paths:      wire.Paths,
		HostFacts:  wire.HostFacts,
		Labels:     wire.Labels,
		Attributes: wire.Attributes,
	}
	if strings.TrimSpace(wire.UpdatedAt) == "" {
		return nil
	}
	updatedAt, err := parseOwnershipTimestamp(wire.UpdatedAt)
	if err != nil {
		return fmt.Errorf("updatedAt: %w", err)
	}
	record.UpdatedAt = updatedAt
	return nil
}

func ResourcePath(root string, record ResourceRecord) (string, error) {
	if err := validateSegment(record.Kind); err != nil {
		return "", fmt.Errorf("kind: %w", err)
	}
	if err := validateSegment(record.Name); err != nil {
		return "", fmt.Errorf("name: %w", err)
	}
	return filepath.Join(root, ResourceDirName, record.Kind, record.Name+".json"), nil
}

func SaveResource(root string, record ResourceRecord) error {
	if strings.TrimSpace(record.APIVersion) == "" {
		record.APIVersion = "bootwright.io/ownership/v1alpha1"
	}
	if strings.TrimSpace(record.Owner) == "" {
		record.Owner = "bootwright"
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now().UTC()
	}
	if err := ValidateResource(record); err != nil {
		return err
	}
	path, err := ResourcePath(root, record)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode ownership resource: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create ownership resource directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("chmod ownership resource directory: %w", err)
	}
	if err := safefs.AtomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write ownership resource: %w", err)
	}
	return nil
}

func LoadResources(root string) ([]ResourceRecord, error) {
	base := filepath.Join(root, ResourceDirName)
	var records []ResourceRecord
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read ownership resource %s: %w", path, err)
		}
		var record ResourceRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return fmt.Errorf("decode ownership resource %s: %w", path, err)
		}
		if err := ValidateResource(record); err != nil {
			return fmt.Errorf("validate ownership resource %s: %w", path, err)
		}
		records = append(records, record)
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Kind != records[j].Kind {
			return records[i].Kind < records[j].Kind
		}
		return records[i].Name < records[j].Name
	})
	return records, nil
}

func ValidateResource(record ResourceRecord) error {
	if err := validateSegment(record.Kind); err != nil {
		return fmt.Errorf("kind: %w", err)
	}
	if err := validateSegment(record.Name); err != nil {
		return fmt.Errorf("name: %w", err)
	}
	for _, values := range []map[string]string{record.HostFacts, record.Labels, record.Attributes} {
		for key, value := range values {
			if sensitiveString(key) || sensitiveString(value) {
				return fmt.Errorf("ownership resource %s/%s contains sensitive field %q", record.Kind, record.Name, key)
			}
		}
	}
	for _, value := range append([]string{record.Owner, record.Context, record.Host, record.Provider, record.Cluster, record.Machine}, record.Paths...) {
		if sensitiveString(value) {
			return fmt.Errorf("ownership resource %s/%s contains sensitive value", record.Kind, record.Name)
		}
	}
	return nil
}

func validateSegment(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("is required")
	}
	if !safeSegmentRE.MatchString(value) || strings.Contains(value, "..") {
		return fmt.Errorf("%q must contain only letters, numbers, dot, dash, or underscore", value)
	}
	return nil
}

func sensitiveString(value string) bool {
	value = strings.ToLower(value)
	for _, marker := range []string{
		"password",
		"token",
		"private_key",
		"private-key",
		"privatekey",
		"bearer ",
		"authorization:",
		"client-secret",
		"kubeconfig",
		"-----begin ",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func parseOwnershipTimestamp(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), nil
		}
	}
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("%q must be RFC3339 or UTC timestamp without timezone", value)
}
