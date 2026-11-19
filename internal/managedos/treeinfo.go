package managedos

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
	"strings"
)

const (
	isoSector            = 2048
	isoDescriptorStart   = 16
	maxVolumeDescriptors = 32
	maxTreeinfoDirectory = 1 << 20
	MaxTreeinfo          = 1 << 20
	MaxDiscinfo          = 1 << 10
)

var (
	errNoRelease = errors.New("the image holds no readable release")
	errNoRecord  = errors.New("the image's root directory names no such file")
)

type MediaBytes interface {
	ReadAt([]byte, int64) (int, error)
}

func ReadMediaRelease(image MediaBytes, size int64) (MediaRelease, error) {
	release, err := readRelease(image, size)
	if errors.Is(err, errNoRelease) || errors.Is(err, errNoRecord) {
		return MediaRelease{}, nil
	}
	if err != nil {
		return MediaRelease{}, err
	}
	if !ValidMediaRelease(release) {
		return MediaRelease{}, nil
	}
	return release, nil
}

type isoImage struct {
	reader MediaBytes
	size   int64
}

func (i isoImage) read(offset, length int64) ([]byte, error) {
	if offset < 0 || length < 0 || offset > i.size || length > i.size-offset {
		return nil, errNoRelease
	}
	buffer := make([]byte, length)
	n, err := i.reader.ReadAt(buffer, offset)
	if n == len(buffer) {
		return buffer, nil
	}
	if err == nil {
		err = mediaError("the image ended before the size the store measured")
	}
	return nil, err
}

func readRelease(reader MediaBytes, size int64) (MediaRelease, error) {
	image := isoImage{reader: reader, size: size}
	root, err := jolietRoot(image)
	if err != nil {
		return MediaRelease{}, err
	}
	directory, err := image.read(root.offset, root.length)
	if err != nil {
		return MediaRelease{}, err
	}
	parse := parseTreeinfo
	file, err := rootFile(directory, treeinfoNames, MaxTreeinfo)
	if errors.Is(err, errNoRecord) {
		parse = parseDiscinfo
		file, err = rootFile(directory, discinfoNames, MaxDiscinfo)
	}
	if err != nil {
		return MediaRelease{}, err
	}
	data, err := image.read(file.offset, file.length)
	if err != nil {
		return MediaRelease{}, err
	}
	release, ok := parse(data)
	if !ok {
		return MediaRelease{}, errNoRelease
	}
	return release, nil
}

type isoExtent struct {
	offset, length int64
	flags          byte
}

func jolietRoot(image isoImage) (isoExtent, error) {
	var found []isoExtent
	for index := range maxVolumeDescriptors {
		descriptor, err := image.read(int64(isoDescriptorStart+index)*isoSector, isoSector)
		if err != nil {
			return isoExtent{}, err
		}
		if !bytes.Equal(descriptor[1:6], []byte("CD001")) {
			return isoExtent{}, errNoRelease
		}
		if descriptor[0] == 255 {
			if len(found) != 1 {
				return isoExtent{}, errNoRelease
			}
			return found[0], nil
		}
		if descriptor[0] != 2 || descriptor[6] != 1 || !jolietEscape(descriptor[88:120]) {
			continue
		}
		if binary.LittleEndian.Uint16(descriptor[128:]) != isoSector || binary.BigEndian.Uint16(descriptor[130:]) != isoSector {
			return isoExtent{}, errNoRelease
		}
		root, ok := directoryRecord(descriptor[156:190])
		if !ok || len(root) != 34 {
			return isoExtent{}, errNoRelease
		}
		extent, ok := recordExtent(root)
		if !ok || extent.flags&0x02 == 0 || extent.length == 0 || extent.length > maxTreeinfoDirectory {
			return isoExtent{}, errNoRelease
		}
		found = append(found, extent)
	}
	return isoExtent{}, errNoRelease
}

func jolietEscape(field []byte) bool {
	if field[0] != '%' || field[1] != '/' || (field[2] != '@' && field[2] != 'C' && field[2] != 'E') {
		return false
	}
	return bytes.Count(field[3:], []byte{0}) == len(field)-3
}

func directoryRecord(data []byte) ([]byte, bool) {
	if len(data) < 34 {
		return nil, false
	}
	length := int(data[0])
	if length < 34 || length > len(data) || 33+int(data[32]) > length || data[32] == 0 {
		return nil, false
	}
	return data[:length], true
}

func recordExtent(record []byte) (isoExtent, bool) {
	location := binary.LittleEndian.Uint32(record[2:])
	length := binary.LittleEndian.Uint32(record[10:])
	if location != binary.BigEndian.Uint32(record[6:]) || length != binary.BigEndian.Uint32(record[14:]) {
		return isoExtent{}, false
	}
	return isoExtent{offset: int64(location) * isoSector, length: int64(length), flags: record[25]}, true
}

var (
	treeinfoNames = [][]byte{ucs2(".treeinfo"), ucs2(".treeinfo;1")}
	discinfoNames = [][]byte{ucs2(".discinfo"), ucs2(".discinfo;1")}
)

func ucs2(name string) []byte {
	encoded := make([]byte, 0, 2*len(name))
	for index := range len(name) {
		encoded = append(encoded, 0, name[index])
	}
	return encoded
}

func rootFile(directory []byte, names [][]byte, bound int64) (isoExtent, error) {
	var found []isoExtent
	for position := 0; position < len(directory); {
		if directory[position] == 0 {
			position = (position/isoSector + 1) * isoSector
			continue
		}
		end := min((position/isoSector+1)*isoSector, len(directory))
		record, ok := directoryRecord(directory[position:end])
		if !ok {
			return isoExtent{}, errNoRelease
		}
		position += len(record)
		name := record[33 : 33+int(record[32])]
		if !bytes.Equal(name, names[0]) && !bytes.Equal(name, names[1]) {
			continue
		}
		extent, ok := recordExtent(record)
		if !ok || extent.flags&0xfe != 0 || record[1] != 0 || record[26] != 0 || record[27] != 0 || extent.length > bound {
			return isoExtent{}, errNoRelease
		}
		found = append(found, extent)
	}
	if len(found) == 0 {
		return isoExtent{}, errNoRecord
	}
	if len(found) != 1 {
		return isoExtent{}, errNoRelease
	}
	return found[0], nil
}

func parseTreeinfo(data []byte) (MediaRelease, bool) {
	sections := map[string]bool{}
	keys := map[string]bool{}
	section, keyed := "", false
	general := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed[0] == '#' || trimmed[0] == ';' {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if !keyed || section == "general" {
				return MediaRelease{}, false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			name, ok := strings.CutSuffix(trimmed[1:], "]")
			if !ok || name == "" || strings.ContainsAny(name, "[]") || sections[name] {
				return MediaRelease{}, false
			}
			sections[name], section, keyed = true, name, false
			clear(keys)
			continue
		}
		separator := strings.IndexAny(trimmed, "=:")
		if section == "" || separator <= 0 {
			return MediaRelease{}, false
		}
		key := strings.ToLower(strings.TrimSpace(trimmed[:separator]))
		if key == "" || keys[key] {
			return MediaRelease{}, false
		}
		keys[key], keyed = true, true
		if section == "general" {
			general[key] = strings.TrimSpace(trimmed[separator+1:])
		}
	}
	release := MediaRelease{Family: general["family"], Version: general["version"], Arch: general["arch"]}
	return release, release.Family != "" && release.Version != "" && release.Arch != ""
}

func parseDiscinfo(data []byte) (MediaRelease, bool) {
	text, terminated := strings.CutSuffix(string(data), "\n")
	lines := strings.Split(text, "\n")
	if !terminated || len(lines) < 3 || len(lines) > 4 {
		return MediaRelease{}, false
	}
	for index := range len(text) {
		if text[index] != '\n' && (text[index] < 0x20 || text[index] > 0x7e) {
			return MediaRelease{}, false
		}
	}
	whole, fraction, dotted := strings.Cut(lines[0], ".")
	if !decimal(whole) || dotted && !decimal(fraction) || !dottedVersion(lines[1]) || !validReleaseValue(lines[2]) {
		return MediaRelease{}, false
	}
	if len(lines) == 4 && (lines[3] == "" || strings.Contains(lines[3], " ")) {
		return MediaRelease{}, false
	}
	return MediaRelease{Version: lines[1], Arch: lines[2]}, true
}

func dottedVersion(value string) bool {
	groups := strings.Split(value, ".")
	return len(groups) >= 2 && !slices.ContainsFunc(groups, func(group string) bool { return !decimal(group) })
}

func decimal(value string) bool {
	return value != "" && strings.Trim(value, "0123456789") == ""
}
