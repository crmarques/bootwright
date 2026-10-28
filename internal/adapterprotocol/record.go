// Package adapterprotocol is the adapter result protocol's one decoder and the
// one runner core every adapter run goes through. A runner adapter supplies
// its invocation and judges each record; what a record means stays its own.
package adapterprotocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Record is one decoded record, carrying exactly its phase's members.
type Record struct {
	Phase       string
	Group       string
	Status      string
	Outcome     string
	Reason      string
	Source      string
	Detail      string
	NativeError *NativeError
	Evidence    json.RawMessage
	Preparation json.RawMessage
	Warnings    []Warning
}

type NativeError struct{ Coordinate, Cause string }

type Warning struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Object  *WarningObject `json:"object,omitempty"`
}

type WarningObject struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

var warningCodes = []string{"substrate.shadowed-route"}

func WarningCodes() []string { return slices.Clone(warningCodes) }

// Admission names the runner a record stream is read for: which phases it
// admits and how many records it reads.
type Admission uint8

const (
	// Lifecycle is a lifecycle adapter run.
	Lifecycle Admission = 1 << iota
	// Controller is a controller Ansible run: setup, its recovery or the
	// controller stage.
	Controller
)

const (
	// MaxLine is the longest record line, its newline included.
	MaxLine = 65536
	// maxDepth is the deepest nesting of objects and arrays a record holds.
	maxDepth             = 16
	maxDetail            = 160
	MaxNativeCoordinate  = 512
	MaxNativeCause       = 1024
	maxLifecycleRecords  = 64
	maxControllerRecords = 132
	MaxWarnings          = 16
	MaxWarningMessage    = 1024
	maxWarningKind       = 63
	maxWarningName       = 253
)

// Records is the most records one run of the admission reads.
func (a Admission) Records() int {
	switch a {
	case Lifecycle:
		return maxLifecycleRecords
	case Controller:
		return maxControllerRecords
	}
	return 0
}

// phase is one phase's exact member set and the runners that admit it. A
// source or detail is an optional refusal member only for its runners.
type phase struct {
	members     []string
	admitted    Admission
	source      Admission
	detail      Admission
	nativeError Admission
	warnings    Admission
}

// phases is the one table of which runner admits which phase.
var phases = map[string]phase{
	"loaded":    {members: []string{"phase"}, admitted: Lifecycle | Controller},
	"group":     {members: []string{"group", "phase", "status"}, admitted: Lifecycle},
	"prepared":  {members: []string{"phase", "preparation"}, admitted: Lifecycle | Controller},
	"native":    {members: []string{"phase"}, admitted: Controller},
	"continue":  {members: []string{"phase"}, admitted: Controller},
	"completed": {members: []string{"evidence", "outcome", "phase"}, admitted: Lifecycle | Controller, warnings: Lifecycle},
	"refused":   {members: []string{"phase", "reason"}, admitted: Lifecycle | Controller, source: Controller, detail: Lifecycle, nativeError: Lifecycle},
}

var (
	errCanonical = errors.New("protocol record is not canonical")
	errRecord    = errors.New("protocol record")
	errPhase     = errors.New("protocol phase")
	errLimit     = errors.New("protocol limit")
)

// Read decodes the result channel for one admission and sends each record
// on. shape is the consumer's own check of a decoded record; its error ends
// the read exactly as a malformed record does. Every record is bounded and
// strictly shaped; each consumer authorizes any native diagnostic data.
func Read(reader io.Reader, admission Admission, shape func(Record) error, out chan<- Record) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), MaxLine)
	scanner.Split(lines)
	for count := 0; scanner.Scan(); count++ {
		if count >= admission.Records() {
			return errLimit
		}
		record, err := Decode(scanner.Bytes(), admission)
		if err != nil {
			return err
		}
		if shape != nil {
			if err := shape(record); err != nil {
				return err
			}
		}
		out <- record
	}
	return scanner.Err()
}

// lines splits at each newline and keeps every other byte, a carriage return
// included, for the decoder to refuse.
func lines(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if index := bytes.IndexByte(data, '\n'); index >= 0 {
		return index + 1, data[:index], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// Decode reads one record line as the collection's canonical writer emits it:
// ASCII, no whitespace outside a string, every object's members in strictly
// ascending order and none two equal under case folding, one object and
// nothing after it, carrying exactly its phase's members.
func Decode(line []byte, admission Admission) (Record, error) {
	if !printable(line) || spaced(line) {
		return Record{}, errCanonical
	}
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return Record{}, errRecord
	}
	if err := walk(decoder, first, 1); err != nil {
		return Record{}, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return Record{}, errRecord
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(line, &members) != nil {
		return Record{}, errRecord
	}
	return shape(members, admission)
}

// printable admits only the bytes Python's ensure_ascii writes.
func printable(line []byte) bool {
	for _, c := range line {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// spaced reports JSON whitespace outside a string.
func spaced(line []byte) bool {
	quoted, escaped := false, false
	for _, c := range line {
		switch {
		case escaped:
			escaped = false
		case quoted && c == '\\':
			escaped = true
		case c == '"':
			quoted = !quoted
		case !quoted && (c == ' ' || c == '\t' || c == '\r' || c == '\n'):
			return true
		}
	}
	return false
}

// walk reads the value token opened and refuses members out of order, members
// equal under case folding and nesting deeper than maxDepth.
func walk(decoder *json.Decoder, token json.Token, depth int) error {
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if depth > maxDepth {
		return errRecord
	}
	closing := json.Delim(']')
	switch delimiter {
	case '{':
		closing = '}'
		previous, seen := "", map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok {
				return errRecord
			}
			folded := strings.ToLower(name)
			if len(seen) > 0 && name <= previous || seen[folded] {
				return errCanonical
			}
			previous, seen[folded] = name, true
			if err := next(decoder, depth); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := next(decoder, depth); err != nil {
				return err
			}
		}
	default:
		return errRecord
	}
	end, err := decoder.Token()
	if err != nil || end != closing {
		return errRecord
	}
	return nil
}

func next(decoder *json.Decoder, depth int) error {
	value, err := decoder.Token()
	if err != nil {
		return errRecord
	}
	return walk(decoder, value, depth+1)
}

// shape holds the members to their phase's exact set and each member to its
// values.
func shape(members map[string]json.RawMessage, admission Admission) (Record, error) {
	var record Record
	if !text(members["phase"], &record.Phase) {
		return Record{}, errPhase
	}
	rule, known := phases[record.Phase]
	if !known || rule.admitted&admission == 0 {
		return Record{}, errPhase
	}
	expected := rule.members
	if _, named := members["source"]; named && rule.source&admission != 0 {
		expected = append(slices.Clone(expected), "source")
	}
	if _, named := members["detail"]; named && rule.detail&admission != 0 {
		expected = append(slices.Clone(expected), "detail")
	}
	if _, named := members["nativeError"]; named && rule.nativeError&admission != 0 {
		expected = append(slices.Clone(expected), "nativeError")
	}
	if _, named := members["warnings"]; named && rule.warnings&admission != 0 {
		expected = append(slices.Clone(expected), "warnings")
	}
	if len(members) != len(expected) {
		return Record{}, errRecord
	}
	for _, name := range expected {
		if _, ok := members[name]; !ok {
			return Record{}, errRecord
		}
	}
	valid := true
	switch record.Phase {
	case "group":
		valid = text(members["group"], &record.Group) && record.Group != "" &&
			text(members["status"], &record.Status) && slices.Contains([]string{"running", "ok", "failed", "skipped"}, record.Status)
	case "completed":
		evidence := members["evidence"]
		valid = text(members["outcome"], &record.Outcome) && (record.Outcome == "changed" || record.Outcome == "unchanged") &&
			len(evidence) > 2 && evidence[0] == '{'
		record.Evidence = evidence
		if raw, named := members["warnings"]; named {
			var ok bool
			record.Warnings, ok = warnings(raw)
			valid = valid && ok
		}
	case "refused":
		valid = text(members["reason"], &record.Reason) && record.Reason != ""
		if raw, named := members["source"]; named {
			valid = valid && text(raw, &record.Source) && record.Source != ""
		}
		if raw, named := members["detail"]; named {
			valid = valid && text(raw, &record.Detail) && detailed(record.Detail)
		}
		if raw, named := members["nativeError"]; named {
			var ok bool
			record.NativeError, ok = nativeError(raw)
			valid = valid && ok
		}
	case "prepared":
		record.Preparation = members["preparation"]
		valid = len(record.Preparation) > 0 && record.Preparation[0] == '{'
	}
	if !valid {
		return Record{}, errRecord
	}
	return record, nil
}

func warnings(raw json.RawMessage) ([]Warning, bool) {
	var entries []json.RawMessage
	if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &entries) != nil || len(entries) == 0 || len(entries) > MaxWarnings {
		return nil, false
	}
	found := make([]Warning, 0, len(entries))
	for _, entry := range entries {
		var members map[string]json.RawMessage
		var warning Warning
		var code, message string
		if len(entry) == 0 || entry[0] != '{' || json.Unmarshal(entry, &members) != nil || json.Unmarshal(entry, &warning) != nil {
			return nil, false
		}
		_, objected := members["object"]
		if len(members) != 2 && !(objected && len(members) == 3) ||
			!text(members["code"], &code) || !slices.Contains(warningCodes, code) ||
			!text(members["message"], &message) || !printableText(message, MaxWarningMessage) {
			return nil, false
		}
		warning.Object = nil
		if objected {
			object, ok := warningObject(members["object"])
			if !ok {
				return nil, false
			}
			warning.Object = object
		}
		found = append(found, warning)
	}
	return found, true
}

func warningObject(raw json.RawMessage) (*WarningObject, bool) {
	var members map[string]json.RawMessage
	if len(raw) == 0 || raw[0] != '{' || json.Unmarshal(raw, &members) != nil || len(members) != 2 {
		return nil, false
	}
	object := &WarningObject{}
	if !text(members["kind"], &object.Kind) || !text(members["name"], &object.Name) || !kindName(object.Kind) || !objectName(object.Name) {
		return nil, false
	}
	return object, true
}

func printableText(value string, limit int) bool {
	if strings.TrimSpace(value) == "" || len(value) > limit {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

func kindName(value string) bool {
	if value == "" || len(value) > maxWarningKind || value[0] < 'A' || value[0] > 'Z' {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func objectName(value string) bool {
	if value == "" || len(value) > maxWarningName || value[0] == '-' || value[0] == '.' || value[len(value)-1] == '-' || value[len(value)-1] == '.' {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

func detailed(value string) bool {
	if value == "" || len(value) > maxDetail {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("_.:@-", c)) {
			return false
		}
	}
	return true
}

// text decodes a member that must be a JSON string.
func text(raw json.RawMessage, target *string) bool {
	return len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, target) == nil
}

func nativeError(raw json.RawMessage) (*NativeError, bool) {
	var members map[string]json.RawMessage
	if len(raw) == 0 || raw[0] != '{' || json.Unmarshal(raw, &members) != nil || len(members) != 2 {
		return nil, false
	}
	result := &NativeError{}
	valid := nativeString(members["coordinate"], &result.Coordinate) && nativeString(members["cause"], &result.Cause) && nativeText(result.Coordinate, MaxNativeCoordinate) && nativeText(result.Cause, MaxNativeCause)
	if !valid {
		return nil, false
	}
	return result, true
}
func nativeText(value string, limit int) bool {
	if strings.TrimSpace(value) == "" || len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func nativeString(raw json.RawMessage, target *string) bool {
	if !text(raw, target) {
		return false
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		if i+1 >= len(raw) {
			return false
		}
		if raw[i+1] != 'u' {
			i++
			continue
		}
		if i+6 > len(raw) {
			return false
		}
		code, err := strconv.ParseUint(string(raw[i+2:i+6]), 16, 16)
		if err != nil {
			return false
		}
		if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
		if code >= 0xd800 && code <= 0xdbff {
			if i+12 > len(raw) || raw[i+6] != '\\' || raw[i+7] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(raw[i+8:i+12]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
		i += 5
	}
	return true
}
