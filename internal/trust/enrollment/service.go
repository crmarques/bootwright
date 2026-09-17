package enrollment

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/trust"
)

// observers bounds how many endpoints one enrollment reads at once, so a wide
// selection stays a bounded amount of concurrent work.
const observers = 8

type Options struct {
	Observer  Observer
	Confirmer Confirmer
	Clock     func() time.Time
}

type Service struct {
	state     EffectiveState
	trust     HostKeyStore
	selection machine.CurrentSelection
	options   Options
}

func New(state EffectiveState, store HostKeyStore, selection machine.CurrentSelection, options Options) Service {
	return Service{state: state, trust: store, selection: selection, options: options}
}

// Enroll records the host keys the selected Machines present. It observes
// exactly the endpoints this context declares, never accepts a changed key
// without an explicit re-trust, and writes nothing at all on a dry run.
func (s Service) Enroll(ctx context.Context, request EnrollRequest) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.state == nil || s.trust == nil || s.options.Observer == nil {
		return nil, availability.ErrNotImplemented
	}
	selected, err := machine.SelectedContext(ctx, s.selection, request.ContextName)
	if err != nil {
		return nil, err
	}
	effective, err := s.state.RenderEffective(ctx, compilation.EffectiveRequest{ContextName: selected})
	if err != nil {
		return nil, err
	}
	chosen, err := candidates(effective.Effective, request.Machines, request.Replace)
	if err != nil {
		return nil, err
	}
	data, err := s.trust.ReadHostKeys(ctx, selected)
	if err != nil {
		return nil, err
	}
	records, err := trust.Decode(data)
	if err != nil {
		return nil, err
	}
	observed, err := s.observe(ctx, chosen)
	if err != nil {
		return nil, err
	}
	report := &Report{Context: selected, DryRun: request.DryRun, Hosts: make([]HostReport, 0, len(chosen))}
	pending := trust.Store{FormatVersion: trust.FormatVersion, Hosts: slices.Clone(records.Hosts)}
	for index, entry := range chosen {
		host, record, err := evaluate(entry, observed[index], records, slices.Contains(request.Replace, entry.name), s.now())
		if err != nil {
			return nil, err
		}
		report.Hosts = append(report.Hosts, host)
		if host.Action == ActionAdd || host.Action == ActionReplace {
			pending.Upsert(record)
			report.Pending++
		}
	}
	if request.DryRun || report.Pending == 0 {
		return report, nil
	}
	if err := s.confirm(ctx, selected, request); err != nil {
		return nil, err
	}
	encoded, err := pending.Encode()
	if err != nil {
		return nil, err
	}
	if err := s.trust.ReplaceHostKeys(ctx, selected, encoded, data); err != nil {
		return nil, err
	}
	report.Recorded = report.Pending
	return report, nil
}

// evaluate decides what one Machine's observation means against what this
// context already trusts. A changed key is never recorded without an explicit
// re-trust: an operator verifies the new fingerprint out of band first.
func evaluate(entry candidate, observed observation, records trust.Store, replace bool,
	now string) (HostReport, trust.Record, error) {
	host := HostReport{Machine: entry.name, Address: entry.address, Port: entry.port}
	if !entry.eligible() {
		host.Action, host.Reason = ActionSkip, entry.skip
		host.Address, host.Port = "", 0
		return host, trust.Record{}, nil
	}
	if observed.err != nil {
		return HostReport{}, trust.Record{}, observed.err
	}
	host.KeyType, host.Fingerprint = observed.key.Type, observed.key.Fingerprint()
	record := trust.Record{
		Machine: entry.name, Address: entry.address, Port: entry.port,
		KeyType: observed.key.Type, PublicKey: observed.key.PublicKey, Fingerprint: host.Fingerprint,
		Source: trust.SourceEnrollment, Recorded: now,
	}
	previous, found := records.Find(entry.name)
	if !found {
		host.Action = ActionAdd
		return host, record, nil
	}
	host.PreviousFingerprint = previous.Fingerprint
	if previous.HostKey() == observed.key && previous.Address == entry.address && previous.Port == entry.port {
		host.Action = ActionReuse
		return host, trust.Record{}, nil
	}
	if !replace {
		return HostReport{}, trust.Record{}, failure("trust.identity",
			"the SSH host key for "+entry.name+" at "+token(entry.address, entry.port)+
				" changed from "+previous.Fingerprint+" to "+host.Fingerprint,
			"verify the new fingerprint out of band, then rerun with --replace "+entry.name)
	}
	host.Action = ActionReplace
	return host, record, nil
}

type observation struct {
	key trust.HostKey
	err error
}

// observe reads every eligible endpoint under a bounded number of workers. A
// skipped Machine is never contacted, because this command has nothing to
// record for it.
func (s Service) observe(ctx context.Context, chosen []candidate) ([]observation, error) {
	out := make([]observation, len(chosen))
	var pending []int
	for index, entry := range chosen {
		if entry.eligible() {
			pending = append(pending, index)
		}
	}
	workers := min(observers, len(pending))
	if workers == 0 {
		return out, ctx.Err()
	}
	queue := make(chan int)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range queue {
				key, err := s.options.Observer.Observe(ctx, chosen[index].address, chosen[index].port)
				out[index] = observation{key: key, err: err}
			}
		}()
	}
	for _, index := range pending {
		if ctx.Err() != nil {
			break
		}
		queue <- index
	}
	close(queue)
	group.Wait()
	return out, ctx.Err()
}

func (s Service) confirm(ctx context.Context, selected string, request EnrollRequest) error {
	if request.SkipConfirmation {
		return nil
	}
	if s.options.Confirmer == nil {
		return failure("trust.identity", "this operation requires confirmation", "repeat the command with --yes")
	}
	return s.options.Confirmer.Confirm(ctx, "trust", selected)
}

func (s Service) now() string {
	clock := s.options.Clock
	if clock == nil {
		clock = time.Now
	}
	return clock().UTC().Format(time.RFC3339)
}
