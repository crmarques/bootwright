package access

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
)

type copyFake struct {
	prepare          func(machine.Session, bool) error
	run              func(context.Context, string) (CopyProgress, error)
	mu               sync.Mutex
	prepared, closed []string
}

func (c *copyFake) Prepare(_ context.Context, session machine.Session, _ CopyRequest, collect bool) (PreparedCopy, error) {
	if c.prepare != nil {
		if err := c.prepare(session, collect); err != nil {
			return nil, err
		}
	}
	c.prepared = append(c.prepared, session.Machine)
	return &preparedFake{copy: c, name: session.Machine}, nil
}

type preparedFake struct {
	copy *copyFake
	name string
}

func (p *preparedFake) Destination() string                           { return "/collection/" + p.name + "/source" }
func (p *preparedFake) Run(ctx context.Context) (CopyProgress, error) { return p.copy.run(ctx, p.name) }
func (p *preparedFake) Close() error {
	p.copy.mu.Lock()
	defer p.copy.mu.Unlock()
	p.copy.closed = append(p.copy.closed, p.name)
	return nil
}

func downloadRequest(names ...string) CopyRequest {
	return CopyRequest{Names: names, Source: machine.CopyPath{Remote: true, Path: "/source"}, Destination: machine.CopyPath{Path: "/collection"}}
}

func TestCopyPreflightsEveryMachineBeforeAnyPayloadWrite(t *testing.T) {
	h := newHarness(t, nil)
	c := &copyFake{prepare: func(s machine.Session, collect bool) error {
		if !collect || !s.Batch {
			t.Error("multi-download was interactive or not collected")
		}
		if s.Machine == "host" {
			return errors.New("destination-exists")
		}
		return nil
	}, run: func(context.Context, string) (CopyProgress, error) {
		t.Error("payload started after refusal")
		return CopyProgress{}, nil
	}}
	h.service.options.Copier = c
	result, err := h.service.Copy(context.Background(), downloadRequest("host", "guest"))
	if err == nil || len(result.Machines) != 0 || len(c.closed) != 1 || c.closed[0] != "guest" {
		t.Fatalf("result %+v error %v prepared %v closed %v", result, err, c.prepared, c.closed)
	}
}

func TestCopyConcurrentFailuresKeepEveryMachineOutcome(t *testing.T) {
	h := newHarness(t, nil)
	started := make(chan string, 3)
	proceed := make(chan struct{})
	c := &copyFake{run: func(_ context.Context, name string) (CopyProgress, error) {
		started <- name
		<-proceed
		if name == "guest" {
			return CopyProgress{Files: 1, Bytes: 3, Unknown: true}, errors.New("lost acknowledgement")
		}
		if name == "host" {
			return CopyProgress{Files: 1}, errors.New("permission denied")
		}
		return CopyProgress{Files: 2, Bytes: 6}, nil
	}}
	h.service.options.Copier = c
	go func() {
		for range 3 {
			<-started
		}
		close(proceed)
	}()
	result, err := h.service.Copy(context.Background(), downloadRequest("host", "guest", "declared", "guest"))
	if err == nil || len(result.Machines) != 3 {
		t.Fatalf("result %+v: %v", result, err)
	}
	for i, name := range []string{"declared", "guest", "host"} {
		row := result.Machines[i]
		want := "copied"
		if name == "guest" {
			want = "unknown"
		}
		if name == "host" {
			want = "failed"
		}
		if row.Machine != name || row.Status != want || row.Files < 1 {
			t.Fatalf("row %+v", row)
		}
	}
	reported := diagnostics.Of(err)
	if len(reported) != 2 || !strings.Contains(reported[0].Message, "guest") || !strings.Contains(reported[1].Message, "host") {
		t.Fatalf("diagnostics %+v", reported)
	}
	if h.lender.lends != 1 || len(c.closed) != 3 {
		t.Fatalf("lends %d closed %v", h.lender.lends, c.closed)
	}
}

func TestCopyUnknownSelectionStartsNoWorker(t *testing.T) {
	h := newHarness(t, nil)
	c := &copyFake{}
	h.service.options.Copier = c
	_, err := h.service.Copy(context.Background(), downloadRequest("guest", "absent"))
	if code(t, err) != "access.target" || len(c.prepared) != 0 || h.lender.lends != 0 {
		t.Fatalf("error %v prepared %v", err, c.prepared)
	}
}

func TestCopyStructuredModeCannotConfirmFirstUseTrust(t *testing.T) {
	h := newHarness(t, nil)
	c := &copyFake{}
	h.service.options.Copier = c
	request := downloadRequest("peer")
	request.NonInteractive = true
	_, err := h.service.Copy(context.Background(), request)
	if code(t, err) != "trust.identity" || len(h.prompt.asked) != 0 || h.observer.probes != 0 || len(h.trust.written) != 0 || len(c.prepared) != 0 || h.errStream.Len() != 0 {
		t.Fatalf("error %v prompts %v observations %d writes %d prepared %v stderr %q", err, h.prompt.asked, h.observer.probes, len(h.trust.written), c.prepared, h.errStream.String())
	}
}

func TestCopyBatchPasswordRefusalNamesTheCopyCommand(t *testing.T) {
	h := newHarness(t, nil)
	h.service.options.Copier = &copyFake{}
	_, err := h.service.Copy(context.Background(), downloadRequest("guest", "secured"))
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "access.unavailable" || !strings.Contains(reported[0].Remediation, "machine cp") || strings.Contains(reported[0].Remediation, "machine exec") || h.lender.lends != 0 {
		t.Fatalf("password refusal %+v lends=%d", reported, h.lender.lends)
	}
}

func TestCopyCancellationStopsQueuedTransfersAndRetainsRows(t *testing.T) {
	h := newHarness(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	running, peak, count := 0, 0, 0
	c := &copyFake{run: func(ctx context.Context, _ string) (CopyProgress, error) {
		mu.Lock()
		running++
		count++
		if running > peak {
			peak = running
		}
		if running == maxConcurrentSessions {
			cancel()
		}
		mu.Unlock()
		<-ctx.Done()
		mu.Lock()
		running--
		mu.Unlock()
		return CopyProgress{Files: 1}, ctx.Err()
	}}
	h.service.options.Copier = c
	sessions := make([]machine.Session, maxConcurrentSessions+3)
	for i := range sessions {
		sessions[i].Machine = fmt.Sprintf("m%02d", i)
	}
	result := &CopyResult{}
	err := h.service.copySessions(ctx, downloadRequest(), sessions, result)
	if !errors.Is(err, context.Canceled) || peak != maxConcurrentSessions || count != maxConcurrentSessions {
		t.Fatalf("peak=%d count=%d: %v", peak, count, err)
	}
	if len(result.Machines) != len(sessions) || result.Machines[len(sessions)-1].Status != "skipped" {
		t.Fatalf("rows %+v", result.Machines)
	}
}
