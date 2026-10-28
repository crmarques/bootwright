package managedos

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math/rand"
	"strings"
	"testing"
)

const rhelTreeinfo = `[header]
type = productmd.treeinfo
version = 1.2

# productmd writes [general] for older readers
[release]
name = Red Hat Enterprise Linux
short = RHEL
version = 9.8

[general]
; compatibility section
family = Red Hat Enterprise Linux
Version: 9.8
arch = x86_64
platforms = x86_64,xen
packagedir =

[tree]
arch = x86_64
`

var rhelRelease = MediaRelease{Family: "Red Hat Enterprise Linux", Version: "9.8", Arch: "x86_64"}

const (
	svdOffset      = 17 * isoSector
	rootOffset     = 20 * isoSector
	treeinfoEntry  = rootOffset + 68
	treeinfoOffset = 21 * isoSector
)

func bothEndian32(field []byte, value uint32) {
	binary.LittleEndian.PutUint32(field, value)
	binary.BigEndian.PutUint32(field[4:], value)
}

func descriptor(image []byte, sector int, kind byte) []byte {
	d := image[sector*isoSector : (sector+1)*isoSector]
	d[0] = kind
	copy(d[1:], "CD001")
	d[6] = 1
	return d
}

func dirRecord(into []byte, location, length uint32, flags byte, name []byte) int {
	size := 33 + len(name)
	size += size % 2
	into[0] = byte(size)
	bothEndian32(into[2:], location)
	bothEndian32(into[10:], length)
	into[25] = flags
	into[28] = 1
	into[32] = byte(len(name))
	copy(into[33:], name)
	return size
}

// isoWith builds an image holding a primary descriptor, a Joliet supplementary
// descriptor at sector 17, the terminator, the Joliet root directory at sector
// 20 and the named file at sector 21.
func isoWith(name []byte, content string) []byte {
	sectors := 22 + len(content)/isoSector
	image := make([]byte, sectors*isoSector)
	descriptor(image, 16, 1)
	svd := descriptor(image, 17, 2)
	copy(svd[88:], "%/E")
	binary.LittleEndian.PutUint16(svd[128:], isoSector)
	binary.BigEndian.PutUint16(svd[130:], isoSector)
	dirRecord(svd[156:], 20, isoSector, 0x02, []byte{0})
	descriptor(image, 18, 255)
	root := image[rootOffset:]
	position := dirRecord(root, 20, isoSector, 0x02, []byte{0})
	position += dirRecord(root[position:], 20, isoSector, 0x02, []byte{1})
	dirRecord(root[position:], 21, uint32(len(content)), 0, name)
	copy(image[treeinfoOffset:], content)
	return image
}

func rhelISO() []byte { return isoWith(ucs2(".treeinfo;1"), rhelTreeinfo) }

func releaseOf(t *testing.T, image []byte) MediaRelease {
	t.Helper()
	release, err := ReadMediaRelease(bytes.NewReader(image), int64(len(image)))
	if err != nil {
		t.Fatalf("reading the release failed: %v", err)
	}
	return release
}

func TestTheReleaseIsReadFromTheJolietTreeinfo(t *testing.T) {
	if got := releaseOf(t, rhelISO()); got != rhelRelease {
		t.Fatalf("release = %+v, want %+v", got, rhelRelease)
	}
	if got := releaseOf(t, isoWith(ucs2(".treeinfo"), rhelTreeinfo)); got != rhelRelease {
		t.Fatalf("release of an unversioned name = %+v, want %+v", got, rhelRelease)
	}
	for _, escape := range []string{"%/@", "%/C"} {
		image := rhelISO()
		copy(image[svdOffset+88:], escape)
		if got := releaseOf(t, image); got != rhelRelease {
			t.Fatalf("release under escape %s = %+v", escape, got)
		}
	}
}

// Every image the reader cannot prove holds one bounded, regular /.treeinfo
// in its Joliet root reads as no release and no error.
func TestAnImageWithoutAReadableTreeinfoHoldsNoRelease(t *testing.T) {
	for name, mutate := range map[string]func([]byte) []byte{
		"no Joliet descriptor":       func(image []byte) []byte { copy(image[svdOffset+88:], "\x00\x00\x00"); return image },
		"a Rock Ridge-only name":     func(image []byte) []byte { return isoWith([]byte("TREEINFO.;1"), rhelTreeinfo) },
		"another file name":          func(image []byte) []byte { return isoWith(ucs2(".treeinfo.bak"), rhelTreeinfo) },
		"an upper-case file name":    func(image []byte) []byte { return isoWith(ucs2(".TREEINFO"), rhelTreeinfo) },
		"no terminator":              func(image []byte) []byte { image[18*isoSector] = 3; return image },
		"a descriptor without CD001": func(image []byte) []byte { image[16*isoSector+1] = 'X'; return image },
		"another sector size":        func(image []byte) []byte { image[svdOffset+129] = 4; return image },
		"disagreeing root location":  func(image []byte) []byte { image[svdOffset+156+6] = 9; return image },
		"a root that is a file":      func(image []byte) []byte { image[svdOffset+156+25] = 0; return image },
		"a root outside the image": func(image []byte) []byte {
			bothEndian32(image[svdOffset+156+2:], 1<<30)
			return image
		},
		"an oversize root directory": func(image []byte) []byte {
			bothEndian32(image[svdOffset+156+10:], maxTreeinfoDirectory+isoSector)
			return image
		},
		"two Joliet descriptors": func(image []byte) []byte {
			copy(image[18*isoSector:19*isoSector], image[svdOffset:svdOffset+isoSector])
			descriptor(image, 19, 255)
			return image
		},
		"a file outside the image": func(image []byte) []byte {
			bothEndian32(image[treeinfoEntry+2:], 10000)
			return image
		},
		"a file larger than its bound": func(image []byte) []byte {
			image = append(image, make([]byte, MaxTreeinfo+isoSector)...)
			bothEndian32(image[treeinfoEntry+10:], MaxTreeinfo+1)
			return image
		},
		"a directory named .treeinfo": func(image []byte) []byte { image[treeinfoEntry+25] = 0x02; return image },
		"a multi-extent file":         func(image []byte) []byte { image[treeinfoEntry+25] = 0x80; return image },
		"an associated file":          func(image []byte) []byte { image[treeinfoEntry+25] = 0x04; return image },
		"an interleaved file":         func(image []byte) []byte { image[treeinfoEntry+26] = 1; return image },
		"extended attributes":         func(image []byte) []byte { image[treeinfoEntry+1] = 1; return image },
		"a record shorter than its name": func(image []byte) []byte {
			image[treeinfoEntry] = 40
			return image
		},
		"a record crossing its sector": func(image []byte) []byte {
			root := image[rootOffset : rootOffset+isoSector]
			clear(root[68:])
			dirRecord(root[isoSector-40:], 21, uint32(len(rhelTreeinfo)), 0, ucs2(".treeinfo;1"))
			return image
		},
		"a directory of zero bytes": func(image []byte) []byte {
			clear(image[rootOffset : rootOffset+isoSector])
			return image
		},
		"a garbage record length": func(image []byte) []byte { image[rootOffset] = 5; return image },
		"two .treeinfo records": func(image []byte) []byte {
			dirRecord(image[treeinfoEntry+56:], 21, uint32(len(rhelTreeinfo)), 0, ucs2(".treeinfo"))
			return image
		},
		"an image shorter than its descriptors": func(image []byte) []byte { return image[:17*isoSector] },
	} {
		t.Run(name, func(t *testing.T) {
			if got := releaseOf(t, mutate(rhelISO())); got.Present() {
				t.Fatalf("release = %+v, want none", got)
			}
		})
	}
}

func TestATreeinfoThatDoesNotNameOneReleaseHoldsNone(t *testing.T) {
	general := "[general]\nfamily = Red Hat Enterprise Linux\nversion = 9.8\narch = x86_64\n"
	for name, content := range map[string]string{
		"a duplicated key":            general + "Family = Other\n",
		"a duplicated section":        general + "[general]\nplatforms = x86_64\n",
		"a duplicated other section":  "[tree]\narch = x\n[tree]\narch = y\n" + general,
		"a missing key":               "[general]\nfamily = Red Hat Enterprise Linux\nversion = 9.8\n",
		"an empty value":              "[general]\nfamily = Red Hat Enterprise Linux\nversion =\narch = x86_64\n",
		"a control character":         "[general]\nfamily = Red Hat\x01 Enterprise Linux\nversion = 9.8\narch = x86_64\n",
		"a tab inside a value":        "[general]\nfamily = Red Hat\tEnterprise Linux\nversion = 9.8\narch = x86_64\n",
		"a non-ASCII value":           "[general]\nfamily = Red Hat Enterprise Linuxé\nversion = 9.8\narch = x86_64\n",
		"an over-long value":          "[general]\nfamily = " + strings.Repeat("a", MaxMediaReleaseValue+1) + "\nversion = 9.8\narch = x86_64\n",
		"a continued value":           general + "  continued\n",
		"a key before every section":  "family = x\n" + general,
		"a line that is no key":       general + "garbage\n",
		"an unterminated section":     "[general\nfamily = x\n",
		"no general section":          "[release]\nname = Red Hat Enterprise Linux\nversion = 9.8\n",
		"a key with no name":          general + "= x86_64\n",
		"a nested section bracket":    "[gen[eral]]\n" + general,
		"an empty file":               "",
		"bytes that are no text file": "\x00\x01\x02",
	} {
		t.Run(name, func(t *testing.T) {
			if got := releaseOf(t, isoWith(ucs2(".treeinfo"), content)); got.Present() {
				t.Fatalf("release = %+v, want none", got)
			}
		})
	}
	crlf := strings.ReplaceAll(general, "\n", "\r\n")
	if got := releaseOf(t, isoWith(ucs2(".treeinfo"), crlf)); got != rhelRelease {
		t.Fatalf("release of CRLF lines = %+v, want %+v", got, rhelRelease)
	}
	continued := "[tree]\narch = x86_64\n  variants = BaseOS\n" + general
	if got := releaseOf(t, isoWith(ucs2(".treeinfo"), continued)); got != rhelRelease {
		t.Fatalf("release beside a continued line of another section = %+v, want %+v", got, rhelRelease)
	}
}

type failingImage struct {
	image []byte
	at    int64
}

func (f failingImage) ReadAt(buffer []byte, offset int64) (int, error) {
	if offset >= f.at {
		return 0, errors.New("input/output error")
	}
	return bytes.NewReader(f.image).ReadAt(buffer, offset)
}

// Only the bytes' own read failure is an error: it is the store's, never the
// image's.
func TestOnlyTheStoresReadFailureIsAnError(t *testing.T) {
	image := rhelISO()
	for _, at := range []int64{16 * isoSector, rootOffset, treeinfoOffset} {
		if _, err := ReadMediaRelease(failingImage{image: image, at: at}, int64(len(image))); err == nil || err.Error() != "input/output error" {
			t.Fatalf("a read failing at %d reported %v, want the store's error", at, err)
		}
	}
	if _, err := ReadMediaRelease(bytes.NewReader(image[:treeinfoOffset]), int64(len(image))); !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("bytes shorter than their measured size reported %v, want a short read", err)
	}
}

// Random damage to the descriptors, the root directory and the file never
// fails the read and never yields a release outside its contract.
func TestRandomlyDamagedImagesNeverFailTheRead(t *testing.T) {
	random := rand.New(rand.NewSource(1))
	regions := [][2]int{{16 * isoSector, 19 * isoSector}, {rootOffset, rootOffset + isoSector}, {treeinfoOffset, treeinfoOffset + len(rhelTreeinfo)}}
	for range 2000 {
		image := rhelISO()
		region := regions[random.Intn(len(regions))]
		for range 1 + random.Intn(8) {
			image[region[0]+random.Intn(region[1]-region[0])] = byte(random.Intn(256))
		}
		release := releaseOf(t, image)
		if !ValidMediaRelease(release) {
			t.Fatalf("a damaged image yielded the invalid release %+v", release)
		}
	}
}

func FuzzReadMediaRelease(f *testing.F) {
	f.Add(rhelISO()[rootOffset:], []byte(rhelTreeinfo))
	f.Fuzz(func(t *testing.T, directory, treeinfo []byte) {
		image := isoWith(ucs2(".treeinfo"), string(treeinfo))
		copy(image[rootOffset:rootOffset+isoSector], directory)
		release := releaseOf(t, image)
		if !ValidMediaRelease(release) {
			t.Fatalf("the image yielded the invalid release %+v", release)
		}
	})
}
