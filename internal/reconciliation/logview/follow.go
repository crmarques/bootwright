package logview

import (
	"context"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func (s Service) Follow(ctx context.Context, selection Selection, sink Sink) (*Summary, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	if err := validate(selection); err != nil {
		return nil, err
	}
	switch {
	case selection.Raw:
		return nil, usage("--follow streams events and output as they are written; --raw reads one whole output")
	case sink == nil:
		return nil, diagnostics.NewFailure("runtime.internal", "a followed log has nowhere to be written", "")
	case selection.Run != "" || selection.Setup != "":
		return s.followStream(ctx, selection, sink)
	}
	name, err := s.resolve(ctx, selection.Context)
	if err != nil {
		return nil, err
	}
	session := newSession(s, name, selection)
	defer session.close()
	if err := session.poll(ctx, true, true); err != nil {
		return nil, err
	}
	follower := &follower{session: session, sink: sink, width: followWidth(session.selected())}
	if err := follower.begin(); err != nil {
		return nil, err
	}
	summary := &Summary{Context: name, Operation: session.operation}
	if session.state != reconciliation.OperationRunning || !session.current {
		summary.State = string(session.state)
		return summary, follower.finish()
	}
	detached, busyPolls := 0, 0
	if !session.mutating {
		detached = 1
	}
	for {
		if err := s.options.Pause(ctx, PollInterval); err != nil {
			_ = follower.finish()
			return nil, err
		}
		err := session.poll(ctx, false, true)
		if busy(err) {
			if busyPolls++; busyPolls >= MaxBusyPolls {
				_ = follower.finish()
				return nil, err
			}
			continue
		}
		if err != nil {
			_ = follower.finish()
			return nil, err
		}
		busyPolls = 0
		if err := follower.emit(); err != nil {
			return nil, err
		}
		summary.State = string(session.state)
		if session.state != reconciliation.OperationRunning {
			return summary, follower.drain()
		}
		detached++
		if session.mutating {
			detached = 0
		}
		if detached >= detachedPolls {
			summary.Warnings = append(summary.Warnings, diagnostics.Diagnostic{Severity: "warning", Code: "logs.detached",
				Message:     "operation " + session.operation + " is running but no apply or destroy holds context " + name,
				Remediation: "continue it with bootwright " + session.verb + " --context " + name})
			return summary, follower.drain()
		}
	}
}

type follower struct {
	session *session
	sink    Sink
	width   int
}

func (f *follower) event(event Event) error {
	return f.sink.Event(Line{Source: event.Block, Text: renderEvent(event, f.width, 0, f.session.selection.FullTimes)})
}

func (f *follower) output(file *tracked, lines [][]byte) error {
	for _, line := range lines {
		if err := f.sink.Output(Line{Source: file.block, Text: string(line)}); err != nil {
			return err
		}
	}
	return nil
}

func (f *follower) begin() error {
	events, err := f.session.history()
	if err != nil {
		return err
	}
	for _, event := range events {
		if err := f.event(event); err != nil {
			return err
		}
	}
	for _, block := range f.session.selected() {
		output := f.session.outputs[block]
		if output == nil {
			continue
		}
		lines, err := output.read()
		if err != nil {
			return err
		}
		if err := f.output(output, lines[max(len(lines)-TailLines, 0):]); err != nil {
			return err
		}
	}
	return nil
}

func (f *follower) events(file *tracked, flush bool) error {
	lines, err := file.read()
	if err != nil {
		return err
	}
	if flush {
		lines = append(lines, file.flush()...)
	}
	for _, line := range lines {
		if entry := f.session.event(file, line); f.session.shown(entry.event, file) {
			if err := f.event(entry.event); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *follower) stream(file *tracked, flush bool) error {
	lines, err := file.read()
	if err != nil {
		return err
	}
	if flush {
		lines = append(lines, file.flush()...)
	}
	return f.output(file, lines)
}

func (f *follower) pass(final bool) error {
	session := f.session
	if err := f.events(session.log, final); err != nil {
		return err
	}
	for _, block := range session.selected() {
		for _, file := range session.files[block] {
			if err := f.events(file, final); err != nil {
				return err
			}
		}
		for _, retired := range session.retiring[block] {
			if err := f.stream(retired, true); err != nil {
				return err
			}
			retired.file.Close()
		}
		delete(session.retiring, block)
		output := session.outputs[block]
		if output == nil {
			continue
		}
		left := session.records[block].State != reconciliation.BlockRunning
		if err := f.stream(output, final || left); err != nil {
			return err
		}
		if left {
			output.file.Close()
			delete(session.outputs, block)
		}
	}
	return nil
}

func (f *follower) emit() error { return f.pass(false) }

func (f *follower) drain() error { return f.pass(true) }

func (f *follower) finish() error {
	session := f.session
	for _, file := range session.jsonl() {
		for _, line := range file.flush() {
			if entry := session.event(file, line); session.shown(entry.event, file) {
				if err := f.event(entry.event); err != nil {
					return err
				}
			}
		}
	}
	for _, block := range session.selected() {
		if output := session.outputs[block]; output != nil {
			if err := f.output(output, output.flush()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s Service) followStream(ctx context.Context, selection Selection, sink Sink) (*Summary, error) {
	stream, name, err := s.openStream(ctx, selection)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	file := &tracked{file: stream, block: stream.Name()}
	summary := &Summary{Context: name}
	if selection.Setup != "" {
		summary.Setup = stream.Name()
	} else {
		summary.Run = stream.Name()
	}
	follower := &follower{sink: sink}
	for {
		held, err := stream.Held(ctx)
		if err != nil {
			return nil, err
		}
		if err := follower.stream(file, !held); err != nil {
			return nil, err
		}
		if !held {
			return summary, nil
		}
		if err := s.options.Pause(ctx, PollInterval); err != nil {
			_ = follower.output(file, file.flush())
			return nil, err
		}
	}
}
