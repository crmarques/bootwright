package controller

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"sync"
)

const ToolTableFormat = "bootwright.controller.tool-table-v1"

const maxPinnedToolBytes int64 = 1 << 30

var pinnedToolKinds = []string{"govc", "helm", "kubectl", "virtctl"}

type ToolRelease struct {
	Version string `json:"version"`
	Bytes   int64  `json:"bytes"`
	SHA256  string `json:"sha256"`
}

type ToolPin struct {
	Kind    string      `json:"kind"`
	Minimum string      `json:"minimum"`
	Default ToolRelease `json:"default"`
}

type toolTableDocument struct {
	Format string    `json:"format"`
	Tools  []ToolPin `json:"tools"`
}

//go:embed tooltable.json
var embeddedToolTable []byte

var embeddedPins = sync.OnceValues(func() ([]ToolPin, error) { return ParseToolTable(embeddedToolTable) })

var releasePattern = regexp.MustCompile(`^v?(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})(.*)$`)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func ParseToolTable(data []byte) ([]ToolPin, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document toolTableDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("the tool table is not one strict JSON document: %w", err)
	}
	if len(bytes.TrimSpace(data[decoder.InputOffset():])) != 0 {
		return nil, errors.New("the tool table holds more than one JSON document")
	}
	if document.Format != ToolTableFormat {
		return nil, fmt.Errorf("the tool table's format is %q, not %q", document.Format, ToolTableFormat)
	}
	kinds := make([]string, 0, len(document.Tools))
	for _, pin := range document.Tools {
		kinds = append(kinds, pin.Kind)
		if !stableRelease(pin.Minimum) || !stableRelease(pin.Default.Version) {
			return nil, fmt.Errorf("the tool table names %s releases that are not MAJOR.MINOR.PATCH without a v", pin.Kind)
		}
		if order, _ := CompareReleases(pin.Default.Version, pin.Minimum); order < 0 {
			return nil, fmt.Errorf("the tool table's default %s %s is older than its minimum %s", pin.Kind, pin.Default.Version, pin.Minimum)
		}
		if !sha256Pattern.MatchString(pin.Default.SHA256) || pin.Default.Bytes <= 0 || pin.Default.Bytes > maxPinnedToolBytes {
			return nil, fmt.Errorf("the tool table's default %s names no lowercase SHA-256 or a size outside its bound", pin.Kind)
		}
		if _, err := hex.DecodeString(pin.Default.SHA256); err != nil {
			return nil, err
		}
	}
	if !slices.Equal(kinds, pinnedToolKinds) {
		return nil, fmt.Errorf("the tool table pins %v, not %v in that order", kinds, pinnedToolKinds)
	}
	return document.Tools, nil
}

func ToolPins() ([]ToolPin, error) {
	pins, err := embeddedPins()
	return slices.Clone(pins), err
}

func PinOf(kind string) (ToolPin, bool, error) {
	pins, err := embeddedPins()
	if err != nil {
		return ToolPin{}, false, err
	}
	index := slices.IndexFunc(pins, func(pin ToolPin) bool { return pin.Kind == kind })
	if index < 0 {
		return ToolPin{}, false, nil
	}
	return pins[index], true, nil
}

func CompareReleases(a, b string) (int, bool) {
	left, right := releasePattern.FindStringSubmatch(a), releasePattern.FindStringSubmatch(b)
	if left == nil || right == nil {
		return 0, false
	}
	for index := 1; index <= 3; index++ {
		x, _ := strconv.Atoi(left[index])
		y, _ := strconv.Atoi(right[index])
		if x != y {
			if x < y {
				return -1, true
			}
			return 1, true
		}
	}
	return compareSuffix(left[4], right[4]), true
}

func compareSuffix(a, b string) int {
	release := func(suffix string) bool { return suffix == "" || suffix[0] == '+' }
	switch {
	case release(a) && release(b):
		return 0
	case release(a):
		return 1
	case release(b):
		return -1
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func stableRelease(version string) bool {
	match := releasePattern.FindStringSubmatch(version)
	return match != nil && match[4] == "" && version[0] != 'v'
}
