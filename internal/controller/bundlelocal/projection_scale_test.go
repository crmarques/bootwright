package bundlelocal

import (
	"fmt"
	"path"
	"slices"
	"testing"
	"time"
)

// A projection is rebuilt from its retained sources for every inspection and
// every publication, and a real closure reaches many thousands of files. The
// collision check therefore has to stay a lookup: the scan of the whole
// projection it replaced took over ten seconds at this file count, three times
// per setup. The bound here is far above the linear cost and far below that.
func TestProjectionCollisionCheckStaysALookup(t *testing.T) {
	const count = 15000
	projected := newProjection()
	payload := make([]byte, 512)
	expected := map[string]bool{}
	names := make([]string, 0, count)
	for i := range count {
		names = append(names, fmt.Sprintf("python/lib/python3.13/site-packages/pkg%03d/sub%02d/module_%05d.py", i/128, (i/16)%8, i))
	}
	for _, name := range names {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			expected[parent] = true
		}
	}
	start := time.Now()
	for _, name := range names {
		if err := projected.add(name, payload, false); err != nil {
			t.Fatal(err)
		}
	}
	elapsed := time.Since(start)
	if len(projected.files) != count {
		t.Fatalf("projected %d of %d files", len(projected.files), count)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("projecting %d files took %v; the collision check is no longer a lookup", count, elapsed)
	}
	// The ancestors recorded while adding must be exactly the directories the
	// closure needs, because publication creates them from this list.
	directories := projected.directories()
	if !slices.IsSorted(directories) || len(directories) != len(expected) {
		t.Fatalf("recorded %d directories for %d ancestors", len(directories), len(expected))
	}
	for _, name := range directories {
		if !expected[name] {
			t.Fatalf("recorded %q, which no file lives under", name)
		}
	}
}

// The recorded ancestors are what refuses a file that collides with a directory,
// so they must answer for every depth rather than only the immediate parent.
func TestProjectionRefusesAFileAtAnyRecordedDirectory(t *testing.T) {
	for _, collision := range []string{"python", "python/lib", "python/lib/site-packages", "python/lib/site-packages/pkg"} {
		t.Run(collision, func(t *testing.T) {
			projected := newProjection()
			if err := projected.add("python/lib/site-packages/pkg/module.py", []byte("module"), false); err != nil {
				t.Fatal(err)
			}
			if err := projected.add(collision, []byte("file"), false); err == nil {
				t.Fatalf("accepted %q although a file already lives under it", collision)
			}
		})
	}
}
