package contextfs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

type reservation struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Name    string `json:"name"`
}

type manifest struct {
	Version              int          `json:"version"`
	ID                   string       `json:"id"`
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
	items := maxIdentities
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
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return state("persisted record is malformed or unsupported")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return state("persisted record contains trailing data")
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
		value = registryRecord(registry)
	case *contexts.Registry:
		value = registryRecord(*registry)
	}
	depth := 8
	switch value.(type) {
	case controllerStateRecord, *controllerStateRecord:
		depth = 20
	}
	if maximum < 1 || !fitsJSON(reflect.ValueOf(value), maximum-1, depth) {
		return nil, state("persisted record exceeds its encoding limit")
	}
	data, err := json.Marshal(value)
	if err != nil || len(data) >= maximum {
		return nil, state("persisted record exceeds its encoding limit")
	}
	return append(data, '\n'), nil
}

// All record types are closed structs of strings, integers, slices and raw
// mutation JSON. Count their exact encoding before allocating the output.
func fitsJSON(value reflect.Value, limit int, depths ...int) bool {
	maxDepth := 8
	if len(depths) != 0 {
		maxDepth = depths[0]
	}
	count := 0
	add := func(n int) bool {
		if n < 0 || n > limit-count {
			return false
		}
		count += n
		return true
	}
	stringSize := func(value string) int {
		size := 2
		for len(value) > 0 {
			r, n := utf8.DecodeRuneInString(value)
			value = value[n:]
			switch {
			case r == '"' || r == '\\' || r == '\b' || r == '\f' || r == '\n' || r == '\r' || r == '\t':
				size += 2
			case r < 0x20 || r == '<' || r == '>' || r == '&' || r == 0x2028 || r == 0x2029 || r == utf8.RuneError && n == 1:
				size += 6
			default:
				size += n
			}
			if size > limit-count {
				return size
			}
		}
		return size
	}
	var visit func(reflect.Value, int) bool
	visit = func(v reflect.Value, depth int) bool {
		if depth > maxDepth || !v.IsValid() {
			return false
		}
		if v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
			if v.IsNil() {
				return add(4)
			}
			return visit(v.Elem(), depth+1)
		}
		if v.Type() == reflect.TypeFor[json.RawMessage]() {
			raw := v.Bytes()
			if len(raw) > maxRecord || !json.Valid(raw) {
				return false
			}
			quoted, escaped := false, false
			for i := 0; i < len(raw); {
				c := raw[i]
				if !quoted && (c == ' ' || c == '\n' || c == '\r' || c == '\t') {
					i++
					continue
				}
				if quoted && !escaped && (c == '<' || c == '>' || c == '&') {
					if !add(6) {
						return false
					}
					i++
					continue
				}
				if quoted && !escaped && c >= utf8.RuneSelf {
					r, n := utf8.DecodeRune(raw[i:])
					size := n
					if r == 0x2028 || r == 0x2029 {
						size = 6
					}
					if !add(size) {
						return false
					}
					i += n
					continue
				}
				if !add(1) {
					return false
				}
				i++
				if escaped {
					escaped = false
					continue
				}
				if quoted && c == '\\' {
					escaped = true
				} else if c == '"' {
					quoted = !quoted
				}
			}
			return true
		}
		switch v.Kind() {
		case reflect.String:
			if v.Len() > limit-count {
				return false
			}
			return add(stringSize(v.String()))
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			value := v.Uint()
			digits := 1
			for value >= 10 {
				value /= 10
				digits++
			}
			return add(digits)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			n := v.Int()
			digits := 1
			u := uint64(n)
			if n < 0 {
				digits++
				u = uint64(-(n + 1)) + 1
			}
			for u >= 10 {
				digits++
				u /= 10
			}
			return add(digits)
		case reflect.Bool:
			if v.Bool() {
				return add(4)
			}
			return add(5)
		case reflect.Slice:
			if v.IsNil() {
				return add(4)
			}
			if !add(2) {
				return false
			}
			for i := 0; i < v.Len(); i++ {
				if i > 0 && !add(1) {
					return false
				}
				if !visit(v.Index(i), depth+1) {
					return false
				}
			}
			return true
		case reflect.Struct:
			if !add(2) {
				return false
			}
			fields := 0
			for i := 0; i < v.NumField(); i++ {
				field := v.Type().Field(i)
				if !field.IsExported() {
					continue
				}
				name := field.Tag.Get("json")
				if name == "-" {
					continue
				}
				if name == "" {
					name = field.Name
				}
				if base, option, found := strings.Cut(name, ","); found {
					if option != "omitempty" {
						return false
					}
					item := v.Field(i)
					empty := false
					switch item.Kind() {
					case reflect.String, reflect.Slice:
						empty = item.Len() == 0
					case reflect.Pointer:
						empty = item.IsNil()
					default:
						return false
					}
					if empty {
						continue
					}
					name = base
				}
				if fields > 0 && !add(1) {
					return false
				}
				fields++
				if !add(stringSize(name)+1) || !visit(v.Field(i), depth+1) {
					return false
				}
			}
			return true
		}
		return false
	}
	return visit(value, 0)
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
	if r.Version != 2 && r.Version != 3 && r.Version != 4 || r.Version == 2 && r.Identities == nil || r.Contexts == nil || len(r.Identities) > maxIdentities || len(r.Contexts) > maxIdentities {
		return state("context registry has unsupported version or bounds")
	}
	if r.Version == 2 && (r.IDNamespace != "" || r.NextIdentity != 0) || r.Version >= 3 && (!validNamespace(r.IDNamespace) || r.NextIdentity == 0 || len(r.Identities) != 0) {
		return state("context registry allocation state is invalid")
	}
	if r.Version < 4 && r.Controller != (contexts.ControllerDescriptor{}) || r.Version == 4 && (r.Controller.Version != 1 || r.Controller.Mode != "initializing" && r.Controller.Mode != "ready" || r.Controller.DirectoryInode == 0 && (r.Controller.DirectoryDevice != 0 || r.Controller.Mode == "ready")) {
		return state("controller store descriptor is invalid")
	}
	ids := make(map[string]bool, len(r.Identities))
	previous := ""
	for _, item := range r.Identities {
		if !identifier(item.ID, "ctx-") || item.ID <= previous {
			return state("context identity ledger is invalid or unordered")
		}
		ids[item.ID] = true
		previous = item.ID
	}
	active := make(map[string]bool, len(r.Contexts))
	previous = ""
	for _, record := range r.Contexts {
		if !contextName(record.Name) || record.Name <= previous || !identifier(record.ID, "ctx-") || r.Version == 2 && !ids[record.ID] || active[record.ID] {
			return state("context name or identity mapping is invalid")
		}
		if r.Version >= 3 && identityNamespace(record.ID) == r.IDNamespace {
			sequence, err := strconv.ParseUint(record.ID[len("ctx-")+16:], 16, 64)
			if err != nil || sequence == 0 || sequence >= r.NextIdentity {
				return state("context identity exceeds its allocation counter")
			}
		}
		if !contextName(record.SecretStoreType) {
			return state("context secret store type is invalid")
		}
		if record.Mode != contexts.Initializing && record.Mode != contexts.Ready && record.Mode != contexts.Deleting {
			return state("context status is invalid")
		}
		if record.EnvironmentDirectory != "" && !canonicalPath(record.EnvironmentDirectory) || record.Revision != "" && !identifier(record.Revision, "rev-") || record.Revision != "" && record.EnvironmentDirectory == "" {
			return state("context input identity is invalid")
		}
		if record.DirectoryInode == 0 && record.DirectoryDevice != 0 || record.Mode != contexts.Initializing && record.DirectoryInode == 0 {
			return state("context directory identity is missing")
		}
		previous = record.Name
		active[record.ID] = true
	}
	return nil
}

func validateReservation(r reservation, id, name string) error {
	if r.Version != 2 || r.ID != id || !identifier(r.ID, "ctx-") || !contextName(r.Name) || name != "" && r.Name != name {
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

func validateManifest(m manifest, id, revision, environment string) error {
	if m.Version != 2 || m.ID != id || m.Revision != revision || !identifier(id, "ctx-") || !identifier(revision, "rev-") || !canonicalPath(m.InputDirectory) || !canonicalPath(m.EnvironmentDirectory) || !beneath(m.InputDirectory, m.EnvironmentDirectory) || m.EnvironmentDirectory != environment || m.Files == nil {
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
