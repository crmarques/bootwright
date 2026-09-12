package operationstore

import (
	"context"
	"encoding/json"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

const (
	MaxLogBytes  = 8 << 20
	maxLogDetail = 512
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
		for _, entry := range entries {
			if !entry.Directory && strings.HasSuffix(entry.Name, ".jsonl") {
				names = append(names, entry.Name)
			}
		}
		slices.Sort(names)
		for _, name := range names {
			paths = append(paths, path.Join(directory, name))
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
