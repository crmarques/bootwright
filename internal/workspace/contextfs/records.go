package contextfs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// ReservationVersion and ManifestVersion identify each record independently of
// the registry. Both dropped the allocated context identity that the context
// name now carries.
const (
	ReservationVersion = 3
	ManifestVersion    = 3
)

type reservation struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
}

type manifest struct {
	Version              int          `json:"version"`
	Context              string       `json:"context"`
	Revision             string       `json:"revision"`
	InputDirectory       string       `json:"inputDirectory"`
	EnvironmentDirectory string       `json:"environmentDirectory"`
	Files                []frozenFile `json:"files"`
}

type frozenFile struct {
	Path     string `json:"path"`
	Category string `json:"category"`
	Size     int    `json:"size"`
	SHA256   string `json:"sha256"`
}

// Comparing the canonical encoding rejects duplicate keys, alternate casing,
// omitted fields, null collections, trailing data and noncanonical spellings.
func decodeRecord(data []byte, maximum int, target any) error {
	if len(data) > maximum || !utf8.Valid(data) {
		return state("persisted record exceeds its bounds or encoding")
	}
	items := maxContexts
	if _, ok := target.(*manifest); ok {
		items = desiredstate.MaxFiles + desiredstate.MaxMarkers
	}
	depth, fields := 8, 16
	if _, ok := target.(*controllerStateRecord); ok {
		depth, fields = 16, 32
	}
	if err := boundedJSONLimits(data, items, depth, fields); err != nil {
		return err
	}
	if registry, ok := target.(*contexts.Registry); ok {
		return decodeRegistry(data, maximum, registry)
	}
	switch err := canonicaljson.DecodeClosed(data, target); {
	case errors.Is(err, canonicaljson.ErrTrailing):
		return state("persisted record contains trailing data")
	case err != nil:
		return state("persisted record is malformed or unsupported")
	}
	canonical, err := encodeRecord(target, maximum)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, data) {
		return state("persisted record is not canonical")
	}
	return nil
}

// Bound representation before encoding/json allocates typed collections. The
// decoder below remains the sole JSON grammar implementation.
func boundedJSON(data []byte, maxItems int) error {
	return boundedJSONLimits(data, maxItems, 8, 16)
}

func boundedJSONLimits(data []byte, maxItems, maxDepth, maxFields int) error {
	type frame struct {
		kind   byte
		commas int
	}
	stack := make([]frame, 0, 8)
	quoted, escaped, start := false, false, 0
	for index, c := range data {
		if quoted {
			if index-start > 6*maxPath {
				return state("persisted string exceeds its limit")
			}
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		switch c {
		case '"':
			quoted, start = true, index
		case '{', '[':
			if len(stack) >= maxDepth {
				return state("persisted record exceeds its nesting limit")
			}
			stack = append(stack, frame{kind: c})
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case ',':
			if len(stack) > 0 {
				top := &stack[len(stack)-1]
				top.commas++
				if top.kind == '[' && top.commas >= maxItems || top.kind == '{' && top.commas >= maxFields {
					return state("persisted collection exceeds its limit")
				}
			}
		case 'n':
			if bytes.HasPrefix(data[index:], []byte("null")) {
				return state("persisted null values are forbidden")
			}
		}
	}
	return nil
}

func encodeRecord(value any, maximum int) ([]byte, error) {
	switch registry := value.(type) {
	case contexts.Registry:
		value = registryDocument(registry)
	case *contexts.Registry:
		value = registryDocument(*registry)
	}
	depth := 8
	switch value.(type) {
	case controllerStateRecord, *controllerStateRecord:
		depth = 20
	}
	if _, fits := canonicaljson.Size(value, maximum-1, depth); maximum < 1 || !fits {
		return nil, state("persisted record exceeds its encoding limit")
	}
	data, err := canonicaljson.Encode(value, canonicaljson.Line)
	if err != nil || len(data)-1 >= maximum {
		return nil, state("persisted record exceeds its encoding limit")
	}
	return data, nil
}

// fitsJSON reports whether a value's exact encoding fits limit bytes, before
// anything allocates it.
func fitsJSON(value reflect.Value, limit int, depths ...int) bool {
	depth := 8
	if len(depths) != 0 {
		depth = depths[0]
	}
	_, fits := canonicaljson.Size(value.Interface(), limit, depth)
	return fits
}

func canonicalPath(path string) bool {
	return len(path) > 0 && len(path) <= maxPath && utf8.ValidString(path) && !strings.ContainsRune(path, 0) && filepath.IsAbs(path) && filepath.Clean(path) == path
}

func beneath(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func identifier(value, prefix string) bool {
	if len(value) != len(prefix)+32 || !strings.HasPrefix(value, prefix) {
		return false
	}
	for _, c := range value[len(prefix):] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func contextName(name string) bool {
	if len(name) == 0 || len(name) > 63 || name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func validateRegistry(r contexts.Registry) error {
	if r.Version != contexts.RegistryVersion || r.Contexts == nil || len(r.Contexts) > maxContexts {
		return state("context registry has unsupported version or bounds")
	}
	descriptor := r.Controller
	if descriptor != (contexts.ControllerDescriptor{}) && (descriptor.Version != 1 || descriptor.Mode != "initializing" && descriptor.Mode != "ready" || descriptor.DirectoryInode == 0 && (descriptor.DirectoryDevice != 0 || descriptor.Mode == "ready")) {
		return state("controller store descriptor is invalid")
	}
	previous := ""
	for _, record := range r.Contexts {
		if !contextName(record.Name) || record.Name <= previous {
			return state("context name is invalid or unordered")
		}
		if !contextName(record.SecretStoreType) {
			return state("context secret store type is invalid")
		}
		if record.Mode != contexts.Initializing && record.Mode != contexts.Ready && record.Mode != contexts.Deleting {
			return state("context status is invalid")
		}
		if record.EnvironmentDirectory != "" && !canonicalPath(record.EnvironmentDirectory) || record.Revision != "" && !identifier(record.Revision, "rev-") || record.Revision != "" && record.EnvironmentDirectory == "" {
			return state("context input provenance is invalid")
		}
		if record.DirectoryInode == 0 && record.DirectoryDevice != 0 || record.Mode != contexts.Initializing && record.DirectoryInode == 0 {
			return state("context directory identity is missing")
		}
		previous = record.Name
	}
	return nil
}

func validateReservation(r reservation, name string) error {
	if r.Version != ReservationVersion || !contextName(r.Name) || name != "" && r.Name != name {
		return state("context reservation contradicts its identity")
	}
	return nil
}

func validFrozenPath(path, category string) bool {
	if path == "" || len(path) > maxPath || !utf8.ValidString(path) || strings.ContainsRune(path, 0) || filepath.IsAbs(path) || filepath.Clean(path) != path || path == "." || path == ".." || strings.HasPrefix(path, "../") {
		return false
	}
	parts := strings.Split(path, "/")
	if len(parts) > desiredstate.MaxPathDepth {
		return false
	}
	for _, part := range parts[:len(parts)-1] {
		if strings.HasPrefix(part, ".") || slices.Contains([]string{"vendor", "node_modules", "playbooks", "roles", "collections", "manifests", "secrets"}, part) {
			return false
		}
	}
	if category == "yaml" {
		return strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")
	}
	return category == "marker" && parts[len(parts)-1] == ".bootwright-addon"
}

func validateManifest(m manifest, name, revision, environment string) error {
	if m.Version != ManifestVersion || m.Context != name || m.Revision != revision || !contextName(name) || !identifier(revision, "rev-") || !canonicalPath(m.InputDirectory) || !canonicalPath(m.EnvironmentDirectory) || !beneath(m.InputDirectory, m.EnvironmentDirectory) || m.EnvironmentDirectory != environment || m.Files == nil {
		return state("input manifest identity is invalid")
	}
	if len(m.Files) > desiredstate.MaxFiles+desiredstate.MaxMarkers {
		return state("input manifest has too many files")
	}
	yamlCount, markerCount, yamlBytes, markerBytes := 0, 0, 0, 0
	previous := ""
	for _, file := range m.Files {
		if !validFrozenPath(file.Path, file.Category) || len(filepath.Join(m.InputDirectory, file.Path)) > maxPath || file.Path <= previous || file.Size < 0 || len(file.SHA256) != 64 {
			return state("input manifest file is invalid or duplicated")
		}
		if file.Category == "marker" {
			parts := strings.Split(filepath.Join(m.InputDirectory, file.Path), "/")
			if len(parts) < 4 || parts[len(parts)-4] != "add-ons" || parts[len(parts)-3] != "_store" {
				return state("input manifest marker is outside its permitted position")
			}
		}
		digest, err := hex.DecodeString(file.SHA256)
		if err != nil || hex.EncodeToString(digest) != file.SHA256 {
			return state("input manifest digest is invalid")
		}
		previous = file.Path
		if file.Category == "yaml" {
			yamlCount++
			yamlBytes += file.Size
			if file.Size > desiredstate.MaxFileBytes {
				return state("frozen YAML exceeds its byte limit")
			}
		} else {
			markerCount++
			markerBytes += file.Size
			if file.Size > desiredstate.MaxMarkerBytes {
				return state("frozen marker exceeds its byte limit")
			}
		}
		if yamlCount > desiredstate.MaxFiles || markerCount > desiredstate.MaxMarkers || yamlBytes > desiredstate.MaxAllFileBytes || markerBytes > desiredstate.MaxAllMarkerBytes {
			return state("frozen input exceeds its acquisition limits")
		}
	}
	return nil
}

func digest(data []byte) string { value := sha256.Sum256(data); return hex.EncodeToString(value[:]) }
