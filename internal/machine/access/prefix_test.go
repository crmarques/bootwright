package access

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Lines of concurrent sessions reach the shared stream whole, each under its
// own Machine's prefix and in the order that session wrote them, however the
// sessions' writes are split and interleaved.
func TestConcurrentSessionLinesNeverInterleave(t *testing.T) {
	var shared bytes.Buffer
	to := newDestination(&shared)
	const writers, lines = 8, 200
	var group sync.WaitGroup
	for i := range writers {
		name := "m" + strconv.Itoa(i)
		group.Go(func() {
			w := to.session(name)
			var stream strings.Builder
			for n := range lines {
				fmt.Fprintf(&stream, "%s line %d %s\n", name, n, strings.Repeat("x", n%37))
			}
			data := []byte(stream.String())
			for len(data) > 0 {
				size := min(len(data), 1+len(data)%7)
				if _, err := w.Write(data[:size]); err != nil {
					t.Error(err)
				}
				data = data[size:]
			}
			w.Close()
		})
	}
	group.Wait()
	next := map[string]int{}
	for _, line := range strings.Split(strings.TrimSuffix(shared.String(), "\n"), "\n") {
		name, rest, found := strings.Cut(line, ": ")
		want := fmt.Sprintf("%s line %d %s", name, next[name], strings.Repeat("x", next[name]%37))
		if !found || rest != want {
			t.Fatalf("line %q, want %q", line, name+": "+want)
		}
		next[name]++
	}
	for i := range writers {
		if got := next["m"+strconv.Itoa(i)]; got != lines {
			t.Fatalf("m%d wrote %d lines, want %d", i, got, lines)
		}
	}
}

// A session that ends mid-line still has that line written, with a line end,
// and a session that ended on a line end writes nothing more.
func TestAnUnterminatedLastLineIsWrittenWhenItsSessionEnds(t *testing.T) {
	var shared bytes.Buffer
	w := newDestination(&shared).session("guest")
	if _, err := w.Write([]byte("first\nsec")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("ond")); err != nil {
		t.Fatal(err)
	}
	if shared.String() != "guest: first\n" {
		t.Fatalf("before the end = %q", shared.String())
	}
	w.Close()
	w.Close()
	if shared.String() != "guest: first\nguest: second\n" {
		t.Fatalf("after the end = %q", shared.String())
	}
}

// A partial line is never held past 64 KiB: reaching that bound writes it as
// its own prefixed line, and a line end directly after it adds no empty line.
func TestALineReaching64KiBIsWrittenAsItsOwnLine(t *testing.T) {
	var shared bytes.Buffer
	w := newDestination(&shared).session("peer")
	long := strings.Repeat("x", maxLineBytes)
	if _, err := w.Write([]byte(long + "tail\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(long)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("\n\nnext\n")); err != nil {
		t.Fatal(err)
	}
	w.Close()
	want := "peer: " + long + "\npeer: tail\npeer: " + long + "\npeer: \npeer: next\n"
	if shared.String() != want {
		got := strings.Split(shared.String(), "\n")
		lengths := make([]int, len(got))
		for i, line := range got {
			lengths[i] = len(line)
		}
		t.Fatalf("line lengths = %v", lengths)
	}
}
