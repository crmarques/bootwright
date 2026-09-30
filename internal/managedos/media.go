package managedos

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func mediaError(message string) error {
	return diagnostics.NewFailure("media.store", message, "")
}

const (
	MediaRecordVersion = 1
	MaxMediaEntries    = 64
	MaxMediaBytes      = 32 << 30
	MaxMediaRecord     = 4 << 10
	// MaxMediaName keeps an image's record name, the image name followed by
	// .json, within one 255-byte file name.
	MaxMediaName = 250
)

// MediaEntry is one image of the host-wide installer media store: its name, the
// exact bytes it holds, the credential-free origin it was acquired from and
// when it was published. It carries no secret and no private path.
type MediaEntry struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Source string `json:"source"`
	Added  string `json:"added"`
}

// ValidMediaName admits one portable ASCII basename ending in a lowercase
// `.iso`, so a media name is safe as a single path segment on every host.
func ValidMediaName(name string) bool {
	if len(name) < 5 || len(name) > MaxMediaName || !strings.HasSuffix(name, ".iso") {
		return false
	}
	stem := name[:len(name)-4]
	if stem == "" || !alphanumeric(stem[0]) || !alphanumeric(stem[len(stem)-1]) {
		return false
	}
	for index := range stem {
		c := stem[index]
		if !alphanumeric(c) && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return !reservedMediaStem(stem)
}

// reservedMediaStem rejects the device names some operating systems resolve
// ahead of a file, so one media store serves every host the same way.
func reservedMediaStem(stem string) bool {
	upper := strings.ToUpper(stem)
	if upper == "CON" || upper == "PRN" || upper == "AUX" || upper == "NUL" {
		return true
	}
	if len(upper) != 4 || upper[3] < '1' || upper[3] > '9' {
		return false
	}
	return strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")
}

func alphanumeric(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// NormalizeMediaDigest accepts the authored spellings of a SHA-256 content pin
// and returns its canonical lowercase hexadecimal form.
func NormalizeMediaDigest(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", true
	}
	value = strings.ToLower(strings.TrimPrefix(strings.ToLower(value), "sha256:"))
	if len(value) != 64 {
		return "", false
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || hex.EncodeToString(decoded) != value {
		return "", false
	}
	return value, true
}

func ValidMediaEntry(entry MediaEntry) bool {
	digest, ok := NormalizeMediaDigest(entry.SHA256)
	if !ok || digest == "" || digest != entry.SHA256 {
		return false
	}
	if !ValidMediaName(entry.Name) || entry.Size < 0 || entry.Size > MaxMediaBytes {
		return false
	}
	if !safeMediaText(entry.Source) || !safeMediaText(entry.Added) {
		return false
	}
	return entry.Source != "" && entry.Added != ""
}

func safeMediaText(value string) bool {
	if len(value) > 512 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	return !strings.ContainsFunc(value, func(c rune) bool { return c < 0x20 || c == 0x7f })
}

// EncodeMediaRecord writes the canonical record published beside an image.
func EncodeMediaRecord(entry MediaEntry) ([]byte, error) {
	if !ValidMediaEntry(entry) {
		return nil, mediaError("media record values are not within their contract")
	}
	data, err := json.Marshal(struct {
		Version int `json:"version"`
		MediaEntry
	}{MediaRecordVersion, entry})
	if err != nil || len(data) > MaxMediaRecord-1 {
		return nil, mediaError("media record could not be canonically encoded")
	}
	return append(data, '\n'), nil
}

// DecodeMediaRecord accepts only the exact canonical bytes EncodeMediaRecord
// produces for the image it is stored beside.
func DecodeMediaRecord(data []byte, name string) (MediaEntry, error) {
	if len(data) > MaxMediaRecord || !bytes.HasSuffix(data, []byte("\n")) {
		return MediaEntry{}, mediaError("media record exceeds its bounds or encoding")
	}
	var record struct {
		Version int `json:"version"`
		MediaEntry
	}
	decoder := json.NewDecoder(bytes.NewReader(data[:len(data)-1]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil || decoder.More() {
		return MediaEntry{}, mediaError("media record is malformed")
	}
	if record.Version != MediaRecordVersion || record.Name != name {
		return MediaEntry{}, mediaError("media record names another image or version")
	}
	canonical, err := EncodeMediaRecord(record.MediaEntry)
	if err != nil || !bytes.Equal(canonical, data) {
		return MediaEntry{}, mediaError("media record is not canonical")
	}
	return record.MediaEntry, nil
}

// DecodeStagedMediaRecord accepts the record retained beside a stage, whose
// hashed name cannot give back the image it names: it reads the name the record
// states and accepts exactly what DecodeMediaRecord accepts for that name.
func DecodeStagedMediaRecord(data []byte) (MediaEntry, error) {
	if len(data) > MaxMediaRecord {
		return MediaEntry{}, mediaError("media record exceeds its bounds or encoding")
	}
	var stated struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &stated); err != nil {
		return MediaEntry{}, mediaError("media record is malformed")
	}
	return DecodeMediaRecord(data, stated.Name)
}

// MediaReservationKey is the shared host claim a context holds while an
// operation of its plan needs one image to stay exactly as it froze it.
func MediaReservationKey(name string) string { return "media:" + name }

// MediaSizeText renders a size for presentation without inventing units.
func MediaSizeText(size int64) string { return strconv.FormatInt(size, 10) }
