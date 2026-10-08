package inputfs

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// untypedScan discovers through files with a listing that reports no entry
// type, as a filesystem without READDIRPLUS does.
func untypedScan(t *testing.T, files *virtualFiles) *discovery {
	t.Helper()
	scan, err := discoverThrough(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { scan.root.Close() })
	scan.listing = func(file *os.File, limit int) ([]directoryEntry, error) {
		entries, err := readDirectoryEntries(file, limit)
		for i := range entries {
			entries[i].kind = syscall.DT_UNKNOWN
		}
		return entries, err
	}
	return scan
}

func writeUntypedInput(t *testing.T, files *virtualFiles) string {
	t.Helper()
	for _, name := range []string{"in/a.yaml", "in/sub/b.yaml", "in/vendor/c.yaml", "in/README.md"} {
		files.write(t, name, name)
	}
	return filepath.Join(files.prefix, "in")
}

func TestReaderClassifiesUntypedEntriesThroughItsOpener(t *testing.T) {
	files := newVirtualFiles(t)
	in := writeUntypedInput(t, files)
	scan := untypedScan(t, files)
	if err := scan.source(in, true); err != nil {
		t.Fatalf("discovery: %#v", diagnostics.Of(err))
	}
	got := make([]string, 0, len(scan.files))
	for path := range scan.files {
		got = append(got, path)
	}
	slices.Sort(got)
	if want := []string{filepath.Join(in, "a.yaml"), filepath.Join(in, "sub", "b.yaml")}; !slices.Equal(got, want) {
		t.Fatalf("discovered %v, want %v", got, want)
	}
	sub, readme := filepath.Join(in, "sub"), filepath.Join(in, "README.md")
	if !slices.ContainsFunc(files.requested(), func(r request) bool { return r.path == sub && r.flags&syscall.O_DIRECTORY != 0 }) {
		t.Fatalf("the untyped directory %s was not opened as a directory through the opener: %v", sub, files.requested())
	}
	if !slices.ContainsFunc(files.requested(), func(r request) bool { return r.path == readme }) {
		t.Fatalf("the untyped entry %s was classified without its opener: %v", readme, files.requested())
	}
}

func TestReaderNamesTheDenialOfAnUntypedEntry(t *testing.T) {
	for _, entry := range []string{"sub", "README.md"} {
		for _, root := range []bool{false, true} {
			t.Run(entry+"/root="+strconv.FormatBool(root), func(t *testing.T) {
				files := newVirtualFiles(t)
				in := writeUntypedInput(t, files)
				denied := filepath.Join(in, entry)
				files.replace = func(path string, _ int) (*os.File, bool, error) {
					if path == denied {
						return nil, true, denial{errno: syscall.EACCES, root: root}
					}
					return nil, false, nil
				}
				scan := untypedScan(t, files)
				message, remediation := accountMessage, accountRemediation
				if root {
					message, remediation = rootMessage, rootRemediation
				}
				assertDenial(t, scan.source(in, true), denied, message, remediation)
			})
		}
	}
}

func TestReaderNamesWhyADirectoryCannotBeEnumerated(t *testing.T) {
	const remediation = "check that the filesystem holding it is mounted and readable, or copy the input to a local directory and name the copy"
	for _, test := range []struct {
		errno                syscall.Errno
		message, remediation string
	}{
		{syscall.EIO, "input directory cannot be enumerated (input/output error)", remediation},
		{syscall.EACCES, accountMessage, accountRemediation},
	} {
		t.Run(test.errno.Error(), func(t *testing.T) {
			files := newVirtualFiles(t)
			in := writeUntypedInput(t, files)
			scan, err := discoverThrough(context.Background(), files)
			if err != nil {
				t.Fatal(err)
			}
			defer scan.root.Close()
			scan.rootReads = false
			scan.listing = func(*os.File, int) ([]directoryEntry, error) { return nil, test.errno }
			assertDenial(t, scan.source(in, true), in, test.message, test.remediation)
		})
	}
}

func direntRecord(ino uint64, kind uint8, name string, padding int) []byte {
	length := 19 + len(name) + 1 + padding
	record := make([]byte, length)
	binary.LittleEndian.PutUint64(record[0:8], ino)
	binary.LittleEndian.PutUint16(record[16:18], uint16(length))
	record[18] = kind
	copy(record[19:], name)
	return record
}

func TestDirectoryEntryRecordsParse(t *testing.T) {
	var buf []byte
	for _, record := range [][]byte{
		direntRecord(1, syscall.DT_DIR, ".", 4),
		direntRecord(2, syscall.DT_DIR, "..", 3),
		direntRecord(3, syscall.DT_UNKNOWN, "untyped.yaml", 0),
		direntRecord(4, syscall.DT_REG, "padded.yml", 7),
		direntRecord(5, syscall.DT_DIR, "sub", 0),
	} {
		buf = append(buf, record...)
	}
	all := []directoryEntry{{"untyped.yaml", syscall.DT_UNKNOWN}, {"padded.yml", syscall.DT_REG}, {"sub", syscall.DT_DIR}}
	for limit := 1; limit <= 4; limit++ {
		got, err := appendDirectoryEntries(buf, nil, limit)
		want := all[:min(limit, len(all))]
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("limit %d: %v (%v), want %v", limit, got, err, want)
		}
	}
	if _, err := appendDirectoryEntries(buf[:len(buf)-1], nil, 10); err == nil {
		t.Fatal("a truncated record was accepted")
	}
}
