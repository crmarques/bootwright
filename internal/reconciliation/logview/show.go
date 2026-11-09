package logview

import (
	"context"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

func (s Service) Show(ctx context.Context, selection Selection) (*Result, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	if err := validate(selection); err != nil {
		return nil, err
	}
	if selection.Run != "" || selection.Setup != "" {
		return s.showStream(ctx, selection)
	}
	name, err := s.resolve(ctx, selection.Context)
	if err != nil {
		return nil, err
	}
	if selection.Raw {
		return s.showRaw(ctx, name, selection)
	}
	session := newSession(s, name, selection)
	defer session.close()
	if err := session.poll(ctx, true, false); err != nil {
		return nil, err
	}
	events, err := session.history()
	if err != nil {
		return nil, err
	}
	return &Result{Context: name, Operation: session.operation, Verb: session.verb, State: string(session.state), Events: events}, nil
}

func (s Service) showRaw(ctx context.Context, name string, selection Selection) (*Result, error) {
	session := newSession(s, name, selection)
	defer session.close()
	var output, log LogFile
	var chosen operationstore.OutputFile
	err := s.workspace.ReadLogs(ctx, name, func(view View) error {
		store := s.store(view)
		if err := session.resolve(ctx, view, store); err != nil {
			return err
		}
		operation, err := store.ReadOperation(ctx, session.operation)
		if err != nil {
			return err
		}
		session.state = operation.State
		if !session.planned {
			if session.blocks, err = blockDirectories(ctx, view, session.operation); err != nil {
				return err
			}
		}
		if !slices.Contains(session.blocks, selection.Block) {
			return notFound("operation "+session.operation+" of context "+name+" froze no block "+selection.Block,
				"run bootwright status --context "+name+" to see the operation's blocks")
		}
		found := false
		if selection.Attempt == 0 {
			if chosen, found, err = store.LatestOutput(ctx, session.operation, selection.Block, 0); err != nil {
				return err
			}
		} else {
			logPath, err := operationstore.AttemptLogPath(session.operation, selection.Block, selection.Attempt, 0)
			if err != nil {
				return err
			}
			chosen.Attempt, chosen.Path, found = selection.Attempt, strings.TrimSuffix(logPath, ".jsonl")+".output", true
		}
		if found {
			if output, found, err = view.Open(ctx, chosen.Path); err != nil {
				return err
			}
		}
		if !found {
			return notFound(retainedNothing(session.operation, selection), "an attempt retains output only once its adapter printed; run bootwright logs --context "+name+" --operation "+session.operation+" --block "+selection.Block+" for its events")
		}
		log, _, err = view.Open(ctx, strings.TrimSuffix(chosen.Path, ".output")+".jsonl")
		return err
	})
	if output != nil {
		defer output.Close()
	}
	if log != nil {
		defer log.Close()
	}
	if err != nil {
		return nil, err
	}
	content, err := readAll(output)
	if err != nil {
		return nil, err
	}
	raw := &Raw{
		Source:  Source{Kind: SourceAttempt, Operation: session.operation, Block: selection.Block, Attempt: chosen.Attempt, Resolution: chosen.Resolution},
		Path:    path.Join(session.reference, chosen.Path),
		Bytes:   int64(len(content)),
		Content: content,
	}
	if raw.Truncated, err = cutShort(log, path.Base(chosen.Path)); err != nil {
		return nil, err
	}
	result := &Result{Context: name, Operation: session.operation, Verb: session.verb, State: string(session.state), Raw: raw}
	if raw.Truncated {
		result.Warnings = append(result.Warnings, diagnostics.Diagnostic{Severity: "warning", Code: "logs.truncated",
			Message: "the retained output " + raw.Path + " was cut short at its bound; what the adapter printed after it is not retained"})
	}
	return result, nil
}

func retainedNothing(operation string, selection Selection) string {
	if selection.Attempt != 0 {
		return "attempt " + strconv.Itoa(selection.Attempt) + " of block " + selection.Block + " of operation " + operation + " retained no output"
	}
	return "block " + selection.Block + " of operation " + operation + " retained no output"
}

func cutShort(log LogFile, output string) (bool, error) {
	if log == nil {
		return false, nil
	}
	file := &tracked{file: log}
	lines, err := file.read()
	if err != nil {
		return false, err
	}
	for _, line := range lines {
		decoded, valid := operationstore.DecodeLogLine(line)
		if valid && decoded.Record.Event == "adapter-output" && strings.HasPrefix(decoded.Record.Detail, output+", ") && strings.HasSuffix(decoded.Record.Detail, ", truncated") {
			return true, nil
		}
	}
	return false, nil
}

func readAll(file LogFile) ([]byte, error) {
	size, err := file.Size()
	if err != nil {
		return nil, err
	}
	return file.ReadRange(0, int(min(size, operationstore.MaxAdapterOutputBytes)))
}

func (s Service) openStream(ctx context.Context, selection Selection) (Stream, string, error) {
	if selection.Setup != "" {
		setup := selection.Setup
		if setup == NewestSetup {
			setup = ""
		}
		if validSetupID(setup) {
			return nil, "", notFound("setup "+setup+" retained no output", "run bootwright logs --setup to read the newest setup's output")
		}
		stream, found, err := s.workspace.OpenSetup(ctx, setup)
		if err == nil && !found {
			err = notFound("this host retains no setup output"+named(setup), "the log root keeps the newest 8 setups' output; run bootwright controller check to see the host's setup")
		}
		return stream, "", err
	}
	name, err := s.resolve(ctx, selection.Context)
	if err != nil {
		return nil, "", err
	}
	stream, found, err := s.workspace.OpenRun(ctx, name, selection.Run)
	if err == nil && !found {
		err = notFound("context "+name+" retains no bounded run "+selection.Run, "the log root keeps each context's newest 16 bounded runs; run bootwright status --context "+name+" to see the context")
	}
	return stream, name, err
}

func named(setup string) string {
	if setup == "" {
		return ""
	}
	return " named " + setup
}

func (s Service) showStream(ctx context.Context, selection Selection) (*Result, error) {
	stream, name, err := s.openStream(ctx, selection)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	content, err := readAll(stream)
	if err != nil {
		return nil, err
	}
	source := Source{Kind: SourceRun, Run: stream.Name()}
	if selection.Setup != "" {
		source = Source{Kind: SourceSetup, Setup: stream.Name()}
	}
	return &Result{Context: name, Raw: &Raw{Source: source, Path: stream.Reference(), Bytes: int64(len(content)), Content: content}}, nil
}
