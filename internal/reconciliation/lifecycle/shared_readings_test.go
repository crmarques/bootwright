package lifecycle

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestASharedReadingRunsOncePerKeyWhileEveryCallerWaitsForIt(t *testing.T) {
	shared := NewSharedReadings()
	var reads atomic.Int32
	release := make(chan struct{})
	read := func(key string) func(context.Context) ([]byte, error) {
		return func(context.Context) ([]byte, error) {
			reads.Add(1)
			<-release
			return []byte(key), nil
		}
	}
	var group sync.WaitGroup
	values := make([][]byte, 12)
	for i := range values {
		key := []string{"first", "second"}[i%2]
		group.Go(func() {
			value, err := shared.Read(context.Background(), key, read(key))
			if err != nil {
				t.Errorf("caller %d: %v", i, err)
			}
			values[i] = value
		})
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	group.Wait()
	if reads.Load() != 2 {
		t.Fatalf("%d readings ran for two keys", reads.Load())
	}
	for i, value := range values {
		if string(value) != []string{"first", "second"}[i%2] {
			t.Fatalf("caller %d read %q", i, value)
		}
	}
	values[0][0] = 'X'
	if value, _ := shared.Read(context.Background(), "first", read("again")); string(value) != "first" {
		t.Fatalf("a caller's copy changed the shared reading to %q", value)
	}
}

func TestAFailedSharedReadingStaysFailedForEveryLaterCaller(t *testing.T) {
	shared := NewSharedReadings()
	refused := errors.New("refused")
	reads := 0
	for range 3 {
		if _, err := shared.Read(context.Background(), "key", func(context.Context) ([]byte, error) { reads++; return nil, refused }); !errors.Is(err, refused) {
			t.Fatalf("read = %v", err)
		}
	}
	if reads != 1 {
		t.Fatalf("a failed reading ran %d times", reads)
	}
	func() {
		defer func() { _ = recover() }()
		_, _ = shared.Read(context.Background(), "panics", func(context.Context) ([]byte, error) { panic("synthetic") })
	}()
	if _, err := shared.Read(context.Background(), "panics", func(context.Context) ([]byte, error) { return []byte("late"), nil }); !errors.Is(err, errReadingAbandoned) {
		t.Fatalf("an abandoned reading read %v", err)
	}
}

func TestACanceledCallerStopsWaitingWhileTheReadingFinishes(t *testing.T) {
	shared := NewSharedReadings()
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan []byte)
	go func() {
		value, _ := shared.Read(context.Background(), "key", func(context.Context) ([]byte, error) {
			close(started)
			<-release
			return []byte("read"), nil
		})
		done <- value
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := shared.Read(ctx, "key", func(context.Context) ([]byte, error) { t.Fatal("a second reading ran"); return nil, nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("a canceled caller read %v", err)
	}
	close(release)
	if value := <-done; string(value) != "read" {
		t.Fatalf("the reading finished with %q", value)
	}
}
