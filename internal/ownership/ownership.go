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

	safefs "github.com/crmarques/bootwright/internal/host/safefs"
)

const ResourceDirName = "resources"

const Owner = "bootwright"

const (
	RoleOwner     = "owner"
	RoleReference = "reference"
)

type ResourceRecord struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	Owner      string            `json:"owner"`
	Role       string            `json:"role,omitempty"`
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

func (record ResourceRecord) EffectiveRole() string {
	if role := strings.TrimSpace(record.Role); role != "" {
		return role
	}
	return RoleOwner
}

func (record ResourceRecord) IsReference() bool {
	return record.EffectiveRole() == RoleReference
}

var safeSegmentRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func (record *ResourceRecord) UnmarshalJSON(data []byte) error {
	var wire struct {
		APIVersion string                     `json:"apiVersion"`
		Kind       string                     `json:"kind"`
		Name       string                     `json:"name"`
		Owner      string                     `json:"owner"`
		Role       string                     `json:"role,omitempty"`
		Context    string                     `json:"context,omitempty"`
		Host       string                     `json:"host,omitempty"`
		Provider   string                     `json:"provider,omitempty"`
		Cluster    string                     `json:"cluster,omitempty"`
		Machine    string                     `json:"machine,omitempty"`
		Paths      []string                   `json:"paths,omitempty"`
		HostFacts  map[string]json.RawMessage `json:"hostFacts,omitempty"`
		Labels     map[string]json.RawMessage `json:"labels,omitempty"`
		Attributes map[string]json.RawMessage `json:"attributes,omitempty"`
		UpdatedAt  string                     `json:"updatedAt"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*record = ResourceRecord{
		APIVersion: wire.APIVersion,
		Kind:       wire.Kind,
		Name:       wire.Name,
		Owner:      wire.Owner,
		Role:       wire.Role,
		Context:    wire.Context,
		Host:       wire.Host,
		Provider:   wire.Provider,
		Cluster:    wire.Cluster,
		Machine:    wire.Machine,
		Paths:      wire.Paths,
		HostFacts:  coerceStringMap(wire.HostFacts),
		Labels:     coerceStringMap(wire.Labels),
		Attributes: coerceStringMap(wire.Attributes),
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

func coerceStringMap(in map[string]json.RawMessage) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, raw := range in {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			out[key] = s
			continue
		}
		out[key] = strings.TrimSpace(string(raw))
	}
	return out
}

func ResourcePath(root string, record ResourceRecord) (string, error) {
	if err := validateSegment(record.Kind); err != nil {
		return "", fmt.Errorf("kind: %w", err)
	}
	if err := validateSegment(record.Name); err != nil {
		return "", fmt.Errorf("name: %w", err)
	}
	base := record.Name
	if record.IsReference() {
		if err := validateSegment(record.Context); err != nil {
			return "", fmt.Errorf("reference record context: %w", err)
		}
		base = record.Name + "@" + record.Context
	}
	return filepath.Join(root, ResourceDirName, record.Kind, base+".json"), nil
}

func SaveResource(root string, record ResourceRecord) error {
	if strings.TrimSpace(record.APIVersion) == "" {
		record.APIVersion = "bootwright.io/ownership/v1alpha1"
	}
	if strings.TrimSpace(record.Owner) == "" {
		record.Owner = Owner
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
	if err := safefs.WriteFileEnsuringDir(path, data, 0o600); err != nil {
		return fmt.Errorf("write ownership resource: %w", err)
	}
	return nil
}

func LoadResources(root string) ([]ResourceRecord, error) {
	records, _, err := LoadResourcesWithWarnings(root)
	return records, err
}

func LoadResourcesWithWarnings(root string) ([]ResourceRecord, []error, error) {
	base := filepath.Join(root, ResourceDirName)
	var records []ResourceRecord
	var warnings []error
	for _, path := range []string{root, base} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		if err != nil {
			return nil, nil, fmt.Errorf("inspect ownership directory %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, []error{fmt.Errorf("skip ownership directory %s: symbolic links are not ownership evidence", path)}, nil
		}
		if !info.IsDir() {
			return nil, []error{fmt.Errorf("skip ownership directory %s: path is not a directory", path)}, nil
		}
	}
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			warnings = append(warnings, fmt.Errorf("skip ownership resource %s: symbolic links are not ownership evidence", path))
			return nil
		}
		if path == base {
			return nil
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		if filepath.Dir(rel) == "." {
			if !entry.IsDir() {
				warnings = append(warnings, fmt.Errorf("skip ownership kind path %s: path is not a directory", path))
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			warnings = append(warnings, fmt.Errorf("inspect ownership resource %s: %w", path, err))
			return nil
		}
		if !info.Mode().IsRegular() {
			warnings = append(warnings, fmt.Errorf("skip ownership resource %s: path is not a regular file", path))
			return nil
		}
		if filepath.Ext(path) != ".json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("read ownership resource %s: %w", path, err))
			return nil
		}
		var record ResourceRecord
		if err := json.Unmarshal(data, &record); err != nil {
			warnings = append(warnings, fmt.Errorf("skip undecodable ownership resource %s: %w", path, err))
			return nil
		}
		if err := ValidateResource(record); err != nil {
			warnings = append(warnings, fmt.Errorf("skip invalid ownership resource %s: %w", path, err))
			return nil
		}
		expectedPath, err := ResourcePath(root, record)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("skip invalid ownership resource %s: resolve canonical path: %w", path, err))
			return nil
		}
		if filepath.Clean(path) != filepath.Clean(expectedPath) {
			warnings = append(warnings, fmt.Errorf("skip ownership resource %s: record identity requires canonical path %s", path, expectedPath))
			return nil
		}
		records = append(records, record)
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, warnings, err
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Kind != records[j].Kind {
			return records[i].Kind < records[j].Kind
		}
		return records[i].Name < records[j].Name
	})
	return records, warnings, nil
}

func LoadContext(dir, contextName string) ([]ResourceRecord, error) {
	records, err := LoadResources(dir)
	if err != nil {
		return nil, err
	}
	return FilterByContext(records, contextName), nil
}

func LoadContextWithWarnings(dir, contextName string) ([]ResourceRecord, []error, error) {
	records, warnings, err := LoadResourcesWithWarnings(dir)
	if err != nil {
		return nil, warnings, err
	}
	return FilterByContext(records, contextName), warnings, nil
}

func FilterByContext(records []ResourceRecord, context string) []ResourceRecord {
	context = strings.TrimSpace(context)
	if context == "" {
		return records
	}
	out := make([]ResourceRecord, 0, len(records))
	for _, record := range records {
		if recordContext := strings.TrimSpace(record.Context); recordContext != "" && recordContext != context {
			continue
		}
		out = append(out, record)
	}
	return out
}

func ValidateResource(record ResourceRecord) error {
	if err := validateSegment(record.Kind); err != nil {
		return fmt.Errorf("kind: %w", err)
	}
	if err := validateSegment(record.Name); err != nil {
		return fmt.Errorf("name: %w", err)
	}
	switch strings.TrimSpace(record.Role) {
	case "", RoleOwner, RoleReference:
	default:
		return fmt.Errorf("ownership resource %s/%s role %q must be one of {%s, %s}", record.Kind, record.Name, record.Role, RoleOwner, RoleReference)
	}
	for _, values := range []map[string]string{record.HostFacts, record.Labels, record.Attributes} {
		for key, value := range values {
			if sensitiveKey(key) || sensitiveValue(value) {
				return fmt.Errorf("ownership resource %s/%s contains sensitive field %q", record.Kind, record.Name, key)
			}
		}
	}
	for _, value := range append([]string{record.Owner, record.Context, record.Host, record.Provider, record.Cluster, record.Machine}, record.Paths...) {
		if sensitiveValue(value) {
			return fmt.Errorf("ownership resource %s/%s contains sensitive value", record.Kind, record.Name)
		}
	}
	for _, path := range record.Paths {
		if strings.Contains(path, "..") {
			return fmt.Errorf("ownership resource %s/%s path %q must not contain %q", record.Kind, record.Name, path, "..")
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

var sensitiveKeyMarkers = []string{
	"password",
	"token",
	"private_key",
	"private-key",
	"privatekey",
	"bearer",
	"authorization",
	"client-secret",
	"kubeconfig",
	"secret",
}

var sensitiveValueMarkers = []string{
	"-----begin ",
	"private_key",
	"privatekey",
	"client-secret",
	"authorization:",
	"bearer ",
}

func sensitiveKey(key string) bool { return containsAnyLower(key, sensitiveKeyMarkers) }
func sensitiveValue(value string) bool {
	return containsAnyLower(value, sensitiveValueMarkers)
}

func containsAnyLower(value string, markers []string) bool {
	value = strings.ToLower(value)
	for _, marker := range markers {
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
