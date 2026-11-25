package logview

import (
	"context"
	"time"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

type ToolLog struct {
	Name    string
	Active  func(group, detail string) bool
	Open    func() (LogFile, string, error)
	Project func([]byte, time.Time) string
}

type toolTail struct {
	source   ToolLog
	since    time.Time
	active   bool
	reported bool
	attempt  int
	identity string
	tail     tracked
	snapshot toolSnapshot
}

type toolSnapshot struct {
	file     LogFile
	identity string
	size     int64
	err      error
}

func (t *toolTail) capture() {
	t.snapshot.file, t.snapshot.identity, t.snapshot.err = t.source.Open()
	if t.snapshot.file != nil && t.snapshot.err == nil {
		t.snapshot.size, t.snapshot.err = t.snapshot.file.Size()
	}
}

func (s *session) releaseToolSnapshots() {
	for _, t := range s.tools {
		if t.snapshot.file != nil {
			t.snapshot.file.Close()
		}
		t.snapshot = toolSnapshot{}
	}
}

func (f *follower) startTool(event Event) {
	if event.Event != "group" || event.Resolution != 0 {
		return
	}
	s := f.session
	t := s.tools[event.Block]
	record := s.records[event.Block]
	if t == nil || event.Attempt != record.Attempts || !t.source.Active(event.Group, event.Detail) {
		return
	}
	if t.active && t.attempt == event.Attempt {
		return
	}
	start, err := time.Parse(time.RFC3339Nano, event.Time)
	if err != nil {
		return
	}
	t.active, t.attempt, t.since = true, event.Attempt, start
	t.identity, t.tail, t.reported = "", tracked{}, false
}

func (f *follower) tools(ctx context.Context, initial, final bool) error {
	s := f.session
	if len(s.tools) == 0 {
		return nil
	}
	defer s.releaseToolSnapshots()
	for _, block := range s.selected() {
		if err := ctx.Err(); err != nil {
			return err
		}
		t := s.tools[block]
		record := s.records[block]
		if t == nil || !t.active {
			continue
		}
		if record.Attempts != t.attempt || (initial && record.State != reconciliation.BlockRunning) {
			t.active = false
			continue
		}
		lines, err := t.read()
		source := block + "/" + t.source.Name
		if err != nil && !t.reported {
			t.reported = true
			if err := f.sink.Output(Line{Source: source, Text: "tool log unavailable; see its reported location"}); err != nil {
				return err
			}
		}
		if initial {
			lines = lines[max(len(lines)-TailLines, 0):]
		}
		for _, text := range lines {
			if err := f.sink.Output(Line{Source: source, Text: text}); err != nil {
				return err
			}
		}
		if final || record.State != reconciliation.BlockRunning {
			t.active = false
		}
	}
	return nil
}

func (t *toolTail) read() ([]string, error) {
	snapshot := t.snapshot
	if snapshot.file == nil || snapshot.err != nil {
		return nil, snapshot.err
	}
	if t.identity != snapshot.identity || snapshot.size < t.tail.offset {
		t.identity, t.tail = snapshot.identity, tracked{}
	}
	if snapshot.size-t.tail.offset > readChunkBytes {
		t.tail = tracked{offset: snapshot.size - readChunkBytes, cut: true}
	}
	if snapshot.size == t.tail.offset {
		return nil, nil
	}
	data, err := snapshot.file.ReadRange(t.tail.offset, int(snapshot.size-t.tail.offset))
	if err != nil {
		return nil, err
	}
	t.tail.offset += int64(len(data))
	var lines []string
	for _, line := range t.tail.split(data) {
		if text := t.source.Project(line, t.since); text != "" {
			lines = append(lines, text)
		}
	}
	return lines, nil
}
