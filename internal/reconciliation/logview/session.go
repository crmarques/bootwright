package logview

import (
	"bytes"
	"context"
	"path"
	"slices"
	"strconv"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

type tracked struct {
	file       LogFile
	path       string
	block      string
	attempt    int
	resolution int
	order      int
	offset     int64
	pending    []byte
	cut        bool
	lastTime   string
	lines      int
}

func (t *tracked) read() ([][]byte, error) {
	size, err := t.file.Size()
	if err != nil {
		return nil, err
	}
	var lines [][]byte
	for t.offset < size {
		data, err := t.file.ReadRange(t.offset, int(min(readChunkBytes, size-t.offset)))
		if err != nil {
			return lines, err
		}
		lines = append(lines, t.split(data)...)
		t.offset += int64(len(data))
		if len(data) == 0 {
			break
		}
	}
	return lines, nil
}

func (t *tracked) split(data []byte) [][]byte {
	var lines [][]byte
	for len(data) > 0 {
		index := bytes.IndexByte(data, '\n')
		part := data
		if index >= 0 {
			part = data[:index]
		}
		if !t.cut {
			if room := MaxLineBytes - len(t.pending); len(part) > room {
				t.pending = append(t.pending, part[:room]...)
				lines = append(lines, t.take())
				t.cut = true
			} else {
				t.pending = append(t.pending, part...)
			}
		}
		if index < 0 {
			break
		}
		if !t.cut {
			lines = append(lines, t.take())
		}
		t.cut = false
		data = data[index+1:]
	}
	return lines
}

func (t *tracked) take() []byte {
	line := bytes.Clone(bytes.TrimSuffix(t.pending, []byte{'\r'}))
	t.pending = t.pending[:0]
	return line
}

func (t *tracked) flush() [][]byte {
	if t.cut || len(t.pending) == 0 {
		t.pending, t.cut = t.pending[:0], false
		return nil
	}
	return [][]byte{t.take()}
}

type keyedEvent struct {
	event Event
	time  string
	order int
	file  *tracked
	line  int
}

type session struct {
	service   Service
	name      string
	selection Selection
	operation string
	verb      string
	blocks    []string
	planned   bool
	reference string
	log       *tracked
	files     map[string][]*tracked
	outputs   map[string]*tracked
	retiring  map[string][]*tracked
	records   map[string]operationstore.BlockRecord
	state     reconciliation.OperationState
	current   bool
	mutating  bool
}

func newSession(service Service, name string, selection Selection) *session {
	return &session{service: service, name: name, selection: selection, files: map[string][]*tracked{},
		outputs: map[string]*tracked{}, retiring: map[string][]*tracked{}, records: map[string]operationstore.BlockRecord{}}
}

func (s *session) close() {
	if s.log != nil {
		s.log.file.Close()
	}
	for _, files := range s.files {
		for _, file := range files {
			file.file.Close()
		}
	}
	for _, output := range s.outputs {
		output.file.Close()
	}
	for _, outputs := range s.retiring {
		for _, output := range outputs {
			output.file.Close()
		}
	}
}

func (s *session) selected() []string {
	if s.selection.Block != "" {
		return []string{s.selection.Block}
	}
	return s.blocks
}

func (s *session) poll(ctx context.Context, first, follow bool) error {
	return s.service.workspace.ReadLogs(ctx, s.name, func(view View) error {
		store := s.service.store(view)
		if first {
			if err := s.resolve(ctx, view, store); err != nil {
				return err
			}
		}
		operation, err := store.ReadOperation(ctx, s.operation)
		if err != nil {
			return err
		}
		index, err := store.Index(ctx)
		if err != nil {
			return err
		}
		s.state, s.current = operation.State, index.Current == s.operation
		if !s.planned {
			if s.blocks, err = blockDirectories(ctx, view, s.operation); err != nil {
				return err
			}
		}
		if first && s.selection.Block != "" && !slices.Contains(s.blocks, s.selection.Block) {
			return notFound("operation "+s.operation+" of context "+s.name+" froze no block "+s.selection.Block,
				"run bootwright status --context "+s.name+" to see the operation's blocks")
		}
		for _, block := range s.selected() {
			record, err := store.Block(ctx, s.operation, block)
			if err != nil {
				return err
			}
			s.records[block] = record
			if err := s.open(ctx, view, block, follow); err != nil {
				return err
			}
		}
		s.mutating = false
		if follow && s.state == reconciliation.OperationRunning {
			s.mutating, err = view.Mutating(ctx)
		}
		return err
	})
}

func (s *session) resolve(ctx context.Context, view View, store OperationStore) error {
	if s.operation = s.selection.Operation; s.operation == "" {
		index, err := store.Index(ctx)
		if err != nil {
			return err
		}
		if s.operation = index.Current; s.operation == "" {
			return notFound("context "+s.name+" has no operation", "run bootwright status --context "+s.name+" to see the context's state")
		}
	}
	entries, err := view.Records().Entries(ctx, s.operation)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return notFound("context "+s.name+" has no operation "+s.operation, "run bootwright status --context "+s.name+" to see the current operation")
	}
	operation, err := store.ReadOperation(ctx, s.operation)
	if err != nil {
		return err
	}
	s.verb, s.reference = string(operation.Verb), view.Logs().Reference()
	if slices.ContainsFunc(entries, func(entry operationstore.Entry) bool { return entry.Name == "plan.json" && !entry.Directory }) {
		plan, err := store.ReadPlan(ctx, s.operation)
		if err != nil {
			return err
		}
		for _, block := range plan.Blocks {
			s.blocks = append(s.blocks, block.ID)
		}
		s.planned = true
	}
	log, found, err := view.Open(ctx, operationstore.OperationLogPath(s.operation))
	if err != nil {
		return err
	}
	if !found {
		return notFound("operation "+s.operation+" of context "+s.name+" has no retained log",
			"the log root keeps the newest 64 operations' logs; run bootwright status --context "+s.name+" for its durable records")
	}
	s.log = &tracked{file: log, path: operationstore.OperationLogPath(s.operation)}
	return nil
}

func blockDirectories(ctx context.Context, view View, operation string) ([]string, error) {
	entries, err := view.Logs().Entries(ctx, path.Join(operation, "blocks"))
	if err != nil {
		return nil, err
	}
	var blocks []string
	for _, entry := range entries {
		if entry.Directory && reconciliation.ValidSegment(entry.Name) {
			blocks = append(blocks, entry.Name)
		}
	}
	return blocks, nil
}

func (s *session) order(block string) int {
	return slices.Index(s.blocks, block) + 1
}

func (s *session) open(ctx context.Context, view View, block string, follow bool) error {
	directory := path.Join(s.operation, "blocks", block)
	entries, err := view.Logs().Entries(ctx, directory)
	if err != nil {
		return err
	}
	record := s.records[block]
	var latest operationstore.AttemptFile
	latestName := ""
	for _, entry := range entries {
		file, valid := operationstore.ParseAttemptFile(entry.Name)
		if entry.Directory || !valid {
			continue
		}
		if file.Output {
			if record.Attempts > 0 && file.Attempt == record.Attempts && (latestName == "" || file.Resolution > latest.Resolution) {
				latest, latestName = file, entry.Name
			}
			continue
		}
		target := path.Join(directory, entry.Name)
		if slices.ContainsFunc(s.files[block], func(known *tracked) bool { return known.path == target }) {
			continue
		}
		handle, found, err := view.Open(ctx, target)
		if err != nil {
			return err
		}
		if found {
			s.files[block] = append(s.files[block], &tracked{file: handle, path: target, block: block, attempt: file.Attempt, resolution: file.Resolution, order: s.order(block)})
		}
	}
	slices.SortFunc(s.files[block], func(x, y *tracked) int {
		if x.attempt != y.attempt {
			return x.attempt - y.attempt
		}
		return x.resolution - y.resolution
	})
	if !follow || record.State != reconciliation.BlockRunning || latestName == "" {
		return nil
	}
	target := path.Join(directory, latestName)
	if known := s.outputs[block]; known != nil {
		if known.path == target {
			return nil
		}
		s.retiring[block] = append(s.retiring[block], known)
		delete(s.outputs, block)
	}
	handle, found, err := view.Open(ctx, target)
	if err != nil || !found {
		return err
	}
	s.outputs[block] = &tracked{file: handle, path: target, block: block, attempt: latest.Attempt, resolution: latest.Resolution}
	return nil
}

func (s *session) event(file *tracked, line []byte) keyedEvent {
	file.lines++
	event := Event{Block: file.block, Attempt: file.attempt, Resolution: file.resolution}
	decoded, valid := operationstore.DecodeLogLine(line)
	switch {
	case !valid:
		event.Event, event.Detail = "unreadable", "a line of "+path.Join(s.reference, file.path)+" is not a log record"
	case decoded.Truncated:
		event.Event, event.Detail = "truncated", strconv.Itoa(decoded.Dropped)+" records dropped"
		if decoded.Dropped == 1 {
			event.Detail = "1 record dropped"
		}
	default:
		record := decoded.Record
		event.Time, event.Event, event.Group, event.Detail = record.Time, record.Event, record.Group, record.Detail
		file.lastTime = record.Time
		if event.Block == "" {
			event.Block = record.Block
		}
		if record.Event == "opened" {
			event.Detail = s.verb + " " + s.operation
			if file.block != "" {
				event.Event, event.Detail = "started", "attempt "+strconv.Itoa(file.attempt)
				if file.resolution != 0 {
					event.Detail += " resolution " + strconv.Itoa(file.resolution)
				}
			}
		}
	}
	return keyedEvent{event: event, time: file.lastTime, order: file.order, file: file, line: file.lines}
}

func (s *session) shown(event Event, file *tracked) bool {
	return s.selection.Block == "" || file != s.log || event.Block == s.selection.Block
}

func (s *session) jsonl() []*tracked {
	files := []*tracked{s.log}
	for _, block := range s.selected() {
		files = append(files, s.files[block]...)
	}
	return files
}

func (s *session) history() ([]Event, error) {
	var keyed []keyedEvent
	for _, file := range s.jsonl() {
		lines, err := file.read()
		if err != nil {
			return nil, err
		}
		for _, line := range lines {
			if entry := s.event(file, line); s.shown(entry.event, file) {
				keyed = append(keyed, entry)
			}
		}
	}
	slices.SortStableFunc(keyed, func(x, y keyedEvent) int {
		switch {
		case x.time != y.time:
			if x.time < y.time {
				return -1
			}
			return 1
		case x.order != y.order:
			return x.order - y.order
		case x.file.attempt != y.file.attempt:
			return x.file.attempt - y.file.attempt
		case x.file.resolution != y.file.resolution:
			return x.file.resolution - y.file.resolution
		}
		return x.line - y.line
	})
	events := make([]Event, 0, len(keyed))
	for _, entry := range keyed {
		events = append(events, entry.event)
	}
	return events, nil
}
