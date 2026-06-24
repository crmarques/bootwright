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

// Owner is the canonical owner stamp for a Bootwright-created ownership record.
// SaveResource defaults Owner to this, and consumers that decide whether
// Bootwright owns a record (for example orphan reporting) compare against it.
// The Ansible ownership_record role writes the same literal independently
// (resource.yml owner: bootwright); keep the two in sync.
const Owner = "bootwright"

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
		APIVersion string                     `json:"apiVersion"`
		Kind       string                     `json:"kind"`
		Name       string                     `json:"name"`
		Owner      string                     `json:"owner"`
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

// coerceStringMap normalizes a wire string map whose values may have been
// serialized as JSON scalars other than strings (for example numeric port
// attributes written by older Ansible roles). String values are taken
// verbatim; numbers and booleans are kept as their literal token so the
// in-memory record stays map[string]string and round-trips as strings.
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
	return filepath.Join(root, ResourceDirName, record.Kind, record.Name+".json"), nil
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

// LoadResources loads every ownership resource record under root. A single file
// that cannot be read, decoded, or validated is SKIPPED rather than failing the
// whole load: the ownership store is the safety net a destroy sweep relies on, so
// one corrupt or policy-rejected record must never hide every other recorded
// resource (and block the very sweep that would reclaim it). Only a genuine
// directory-traversal failure is returned as an error. Use LoadResourcesWithWarnings
// to surface which records were skipped.
func LoadResources(root string) ([]ResourceRecord, error) {
	records, _, err := LoadResourcesWithWarnings(root)
	return records, err
}

// LoadResourcesWithWarnings is LoadResources plus the per-record skip reasons, so
// a caller (state-check, destroy preview) can report which records were dropped
// instead of letting them vanish silently.
func LoadResourcesWithWarnings(root string) ([]ResourceRecord, []error, error) {
	base := filepath.Join(root, ResourceDirName)
	var records []ResourceRecord
	var warnings []error
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".json" {
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

// FilterByContext drops records explicitly stamped with a different context, so a
// destroy never consumes an ownership record that belongs to another Bootwright
// context (for example a record misplaced into a shared context directory).
// Records with no context stamp are kept for backward compatibility with records
// written before the context field existed.
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
	// Owned paths drive destructive removal during destroy; reject `..` so a
	// recorded path cannot traverse outside its intended root when the record is
	// later consumed to delete files.
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
