package enrollment

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
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
	Presenter PlanPresenter
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
	chosen, err := candidates(effective.Effective, request.Machines, request.Replace, selected)
	if err != nil {
		return nil, err
	}
	data, err := s.trust.ReadHostKeys(ctx, selected)
	if err != nil {
		return nil, err
	}
	records, err := trust.Decode(data)
	if err != nil {
		return nil, retrust(err, selected, nil, effective.Effective)
	}
	observed, err := s.observe(ctx, chosen)
	if err != nil {
		return nil, err
	}
	report := &Report{Context: selected, DryRun: request.DryRun, Hosts: make([]HostReport, 0, len(chosen))}
	pending := records.Clone()
	exempt := exemptMachines(effective.Effective)
	uses := usingMachines(effective.Effective)
	writing := map[string]bool{}
	var removed []HostReport
	for index, entry := range chosen {
		host, record, err := evaluate(entry, observed[index], records, slices.Contains(request.Replace, entry.name), selected, s.now())
		if err != nil {
			return nil, err
		}
		report.Hosts = append(report.Hosts, host)
		if host.Action == ActionAdd || host.Action == ActionReplace {
			writing[entry.name] = true
			for _, stale := range pending.Supersede(record, uses) {
				removed = append(removed, removal(stale, exempt(stale.Machine), entry.name))
			}
			report.Pending++
		}
	}
	slices.SortFunc(removed, func(a, b HostReport) int { return strings.Compare(a.Machine, b.Machine) })
	report.Hosts = append(report.Hosts, removed...)
	report.Pending += len(removed)
	if err := pending.Validate(); err != nil {
		return nil, retrust(err, selected, func(name string) bool { return writing[name] }, effective.Effective)
	}
	if request.DryRun || report.Pending == 0 {
		return report, nil
	}
	if err := s.confirm(ctx, selected, request, report); err != nil {
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
// re-trust: an operator verifies the new fingerprint out of band first. An
// unchanged key at another endpoint is refused too, since the store trusts a
// key at one address, but named as a move rather than a changed key.
func evaluate(entry candidate, observed observation, records trust.Store, replace bool,
	contextName, now string) (HostReport, trust.Record, error) {
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
	unchanged := previous.HostKey() == observed.key
	moved := previous.Address != entry.address || previous.Port != entry.port
	if unchanged && !moved {
		host.Action = ActionReuse
		return host, trust.Record{}, nil
	}
	if !replace && unchanged {
		return HostReport{}, trust.Record{}, failure("trust.identity",
			"the SSH host key for "+entry.name+" is unchanged ("+host.Fingerprint+"), but this context trusts it at "+
				token(previous.Address, previous.Port)+", not at "+token(entry.address, entry.port),
			"if "+entry.name+" moved, rerun bootwright machine trust --context "+contextName+" --replace "+entry.name+
				" to trust its key at the new address")
	}
	if !replace {
		return HostReport{}, trust.Record{}, failure("trust.identity",
			"the SSH host key for "+entry.name+" at "+token(entry.address, entry.port)+
				" changed from "+previous.Fingerprint+" to "+host.Fingerprint,
			"verify the new fingerprint out of band, then rerun bootwright machine trust --context "+contextName+
				" --machines "+entry.name+" --replace "+entry.name)
	}
	host.Action = ActionReplace
	if moved {
		host.PreviousAddress, host.PreviousPort = previous.Address, previous.Port
	}
	return host, record, nil
}

// removal reports a record the write removes because a selected Machine now
// holds its endpoint and nothing reads the record any more: its Machine is no
// longer declared, or exemption says why it no longer uses this store.
func removal(stale trust.Record, exemption, successor string) HostReport {
	reason := "no longer declared"
	if exemption != "" {
		reason = exemption + ", so it no longer uses this context's SSH trust"
	}
	return HostReport{
		Machine: stale.Machine, Address: stale.Address, Port: stale.Port, Action: ActionRemove,
		KeyType: stale.KeyType, Fingerprint: stale.HostKey().Fingerprint(),
		Reason: reason + "; " + successor + " now uses its address",
	}
}

// usingMachines reports whether the context still reads a Machine's record:
// it declares the Machine and the Machine is not exempt from its trust.
func usingMachines(catalog api.Catalog) func(string) bool {
	return func(name string) bool {
		object, found := catalog.Find(api.Machine, name)
		return found && machine.TrustExemption(object) == ""
	}
}

// retrust names the re-trust that settles a pin the write would leave
// divergent, in this context.
func retrust(err error, contextName string, writing func(string) bool, catalog api.Catalog) error {
	var pin *trust.DivergentPin
	if errors.As(err, &pin) {
		return pin.Retrust(contextName, writing, exemptMachines(catalog))
	}
	return err
}

func exemptMachines(catalog api.Catalog) func(string) string {
	return func(name string) string {
		if object, found := catalog.Find(api.Machine, name); found {
			return machine.TrustExemption(object)
		}
		return ""
	}
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

// confirm shows the operator the one evaluated report it then records, and
// never asks without showing it first.
func (s Service) confirm(ctx context.Context, selected string, request EnrollRequest, report *Report) error {
	if request.SkipConfirmation {
		return nil
	}
	if s.options.Confirmer == nil {
		return failure("trust.identity", "this operation requires confirmation", "repeat the command with --yes")
	}
	if s.options.Presenter == nil {
		return failure("trust.identity", "host-key trust plan presentation is not configured", "")
	}
	if err := s.options.Presenter.PresentTrustPlan(ctx, *report); err != nil {
		return err
	}
	report.Presented = true
	return s.options.Confirmer.Confirm(ctx, "trust", selected)
}

func (s Service) now() string {
	clock := s.options.Clock
	if clock == nil {
		clock = time.Now
	}
	return clock().UTC().Format(time.RFC3339)
}
