package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"sync"
)

type SharedReadings struct {
	mutex    sync.Mutex
	readings map[string]*sharedReading
}

type sharedReading struct {
	done  chan struct{}
	value []byte
	err   error
}

var errReadingAbandoned = errors.New("the shared reading was abandoned")

func NewSharedReadings() *SharedReadings {
	return &SharedReadings{readings: map[string]*sharedReading{}}
}

func (s *SharedReadings) Read(ctx context.Context, key string, read func(context.Context) ([]byte, error)) ([]byte, error) {
	s.mutex.Lock()
	reading, found := s.readings[key]
	if !found {
		reading = &sharedReading{done: make(chan struct{}), err: errReadingAbandoned}
		s.readings[key] = reading
	}
	s.mutex.Unlock()
	if !found {
		func() {
			defer close(reading.done)
			value, err := read(ctx)
			reading.value, reading.err = value, err
		}()
		return bytes.Clone(reading.value), reading.err
	}
	select {
	case <-reading.done:
		return bytes.Clone(reading.value), reading.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
