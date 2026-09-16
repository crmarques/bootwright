package operationstore

import (
	"context"
	"encoding/json"
	"path"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

const (
	MaxLogBytes = 8 << 20
	// MaxAdapterOutputBytes bounds one run's retained output. It stays below
	// the area's own per-file limit, so the bound is this store's decision to
	// truncate rather than a write the area refuses.
	MaxAdapterOutputBytes = 4 << 20
	// adapterOutputFlush bounds what a running adapter's output holds in
	// memory, so a run that wedges is readable before it ends.
	adapterOutputFlush = 8 << 10
	maxLogDetail       = 512
)

// LogRecord is the only shape a private operation log accepts. Every field is
// bounded normalized product vocabulary; raw adapter bytes, terminal control
// sequences and anything an adapter marked sensitive never reach it.
type LogRecord struct {
	Time   string `json:"time"`
	Event  string `json:"event"`
	Block  string `json:"block,omitempty"`
	Group  string `json:"group,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Log is troubleshooting material, never ownership evidence or a continuation
// cursor. It reports its own faults so the caller can stop new work.
type Log struct {
	area    Area
	stamp   func() (string, error)
	path    string
	written int
	dropped int
	closed  bool
}

func (l *Log) Path() string { return l.path }

func OperationLogPath(id string) string { return path.Join(id, "logs", "operation.jsonl") }

// LogDirectory names where one operation's logs are written on the host, so a
// result can point an operator at work still in flight. It is empty when the
// area reports no host location.
func (s *Store) LogDirectory(id string) string {
	location := s.area.Location()
	if location == "" || id == "" {
		return ""
	}
	return path.Join(location, id, "logs")
}

func AttemptLogPath(id, block string, attempt, resolution int) (string, error) {
	name, err := reconciliation.FormatNumber(attempt)
	if err != nil {
		return "", err
	}
	file := "attempt-" + name
	if resolution != 0 {
		suffix, err := reconciliation.FormatNumber(resolution)
		if err != nil {
			return "", err
		}
		file += "-resolution-" + suffix
	}
	return path.Join(id, "logs", "blocks", block, file+".jsonl"), nil
}

// AdapterOutput retains what one adapter process printed on its own standard
// output and error, raw and unparsed. It is troubleshooting material only:
// nothing reads it back, and neither a full buffer nor a failed write ever
// changes the outcome the operation records. The adapter marks every task that
// touches bound material no_log, so its own output carries none; that masking,
// not a cutoff here, is what keeps material out of a retained run.
type AdapterOutput struct {
	// ctx is held because io.Writer has none and the writes arrive on the
	// adapter process's own goroutine. Close takes the live one instead.
	ctx       context.Context
	area      Area
	path      string
	mutex     sync.Mutex
	pending   []byte
	written   int
	truncated bool
	closed    bool
}

// OpenAdapterOutput prepares the retained output beside an already open attempt
// log, whose own directory it shares. It cannot fail, because retention is not
// a precondition of the effect it records.
func (s *Store) OpenAdapterOutput(ctx context.Context, target string) *AdapterOutput {
	return &AdapterOutput{ctx: ctx, area: s.area, path: target}
}

func (o *AdapterOutput) Write(value []byte) (int, error) {
	size := len(value)
	if o == nil {
		return size, nil
	}
	o.mutex.Lock()
	defer o.mutex.Unlock()
	if o.closed {
		return size, nil
	}
	if room := MaxAdapterOutputBytes - o.written - len(o.pending); room < size {
		o.truncated = true
		value = value[:max(room, 0)]
	}
	o.pending = append(o.pending, value...)
	if len(o.pending) >= adapterOutputFlush {
		_ = o.flush(o.ctx)
	}
	return size, nil
}

// Close publishes whatever the run left buffered under the caller's own live
// context, because the one the run held may already be canceled.
func (o *AdapterOutput) Close(ctx context.Context) error {
	if o == nil {
		return nil
	}
	o.mutex.Lock()
	defer o.mutex.Unlock()
	if o.closed {
		return nil
	}
	o.closed = true
	return o.flush(ctx)
}

// Retained reports the bytes this run published and whether its bound cut the
// output short, so the attempt log can record exactly what is on disk.
func (o *AdapterOutput) Retained() (int, bool) {
	if o == nil {
		return 0, false
	}
	o.mutex.Lock()
	defer o.mutex.Unlock()
	return o.written, o.truncated
}

func (o *AdapterOutput) flush(ctx context.Context) error {
	if len(o.pending) == 0 {
		return nil
	}
	if err := o.area.Append(ctx, o.path, o.pending); err != nil {
		return err
	}
	o.written += len(o.pending)
	o.pending = o.pending[:0]
	return nil
}

// AdapterOutputPath names the retained output beside the attempt log it
// belongs to, so one attempt's evidence stays together.
func AdapterOutputPath(logPath string) (string, error) {
	trimmed, found := strings.CutSuffix(logPath, ".jsonl")
	if !found || trimmed == "" {
		return "", recordError("an adapter output path requires its attempt log")
	}
	return trimmed + ".output", nil
}

// OpenLog establishes the logging boundary before its effect or observation
// runs. A failure here is a durable operation fault, not a warning.
func (s *Store) OpenLog(ctx context.Context, target string) (*Log, error) {
	if err := s.area.EnsureDirectory(ctx, path.Dir(target)); err != nil {
		return nil, err
	}
	log := &Log{area: s.area, stamp: s.now, path: target}
	if err := log.Append(ctx, LogRecord{Event: "opened"}); err != nil {
		return nil, err
	}
	return log, nil
}

func (l *Log) Append(ctx context.Context, record LogRecord) error {
	if l == nil || l.closed {
		return recordError("the private operation log is not open")
	}
	stamp, err := l.stamp()
	if err != nil {
		return err
	}
	record.Time = stamp
	record.Event = safeLogText(record.Event, maxLogDetail)
	record.Block = safeLogText(record.Block, maxLogDetail)
	record.Group = safeLogText(record.Group, maxLogDetail)
	record.Detail = safeLogText(record.Detail, maxLogDetail)
	if record.Event == "" {
		return recordError("a private operation log record requires its event")
	}
	line, err := json.Marshal(record)
	if err != nil {
		return recordError("a private operation log record could not be encoded")
	}
	line = append(line, '\n')
	if l.written+len(line) > MaxLogBytes {
		l.dropped++
		return nil
	}
	if err := l.area.Append(ctx, l.path, line); err != nil {
		return err
	}
	l.written += len(line)
	return nil
}

// Close makes truncation explicit: a reader must never mistake a bounded log
// for a complete one.
func (l *Log) Close(ctx context.Context) error {
	if l == nil || l.closed {
		return nil
	}
	dropped := l.dropped
	l.closed = true
	if dropped == 0 {
		return nil
	}
	line, err := json.Marshal(struct {
		Truncated bool `json:"truncated"`
		Dropped   int  `json:"dropped"`
	}{true, dropped})
	if err != nil {
		return recordError("the private operation log truncation marker could not be encoded")
	}
	return l.area.Append(ctx, l.path, append(line, '\n'))
}

// LogPaths lists created logs once, operation log first, then blocks in frozen
// plan order and their attempts and resolutions in numeric order.
func (s *Store) LogPaths(ctx context.Context, id string, plan reconciliation.Plan) ([]string, error) {
	var paths []string
	if _, found, err := s.area.Read(ctx, OperationLogPath(id), 0); err != nil {
		return nil, err
	} else if found {
		paths = append(paths, OperationLogPath(id))
	}
	for _, block := range plan.Blocks {
		directory := path.Join(id, "logs", "blocks", block.ID)
		entries, err := s.area.Entries(ctx, directory)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(entries))
		outputs := map[string]bool{}
		for _, entry := range entries {
			if entry.Directory {
				continue
			}
			if strings.HasSuffix(entry.Name, ".jsonl") {
				names = append(names, entry.Name)
			}
			if strings.HasSuffix(entry.Name, ".output") {
				outputs[entry.Name] = true
			}
		}
		slices.Sort(names)
		for _, name := range names {
			paths = append(paths, path.Join(directory, name))
			if retained, found := outputs[strings.TrimSuffix(name, ".jsonl")+".output"]; found && retained {
				paths = append(paths, path.Join(directory, strings.TrimSuffix(name, ".jsonl")+".output"))
			}
		}
	}
	return paths, nil
}

func safeLogText(value string, limit int) string {
	if value == "" {
		return ""
	}
	if !utf8.ValidString(value) || len(value) > limit {
		value = strings.ToValidUTF8(value, "")
		if len(value) > limit {
			value = value[:limit]
			for !utf8.ValidString(value) && len(value) > 0 {
				value = value[:len(value)-1]
			}
		}
	}
	return strings.Map(func(c rune) rune {
		if c < 0x20 || c == 0x7f {
			return -1
		}
		return c
	}, value)
}
