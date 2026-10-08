package access

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
)

// hostKey proves the server identity before any credential is offered, from
// exactly one source. The order is what makes the proof meaningful: a key this
// context declared or installed outranks one it merely recorded, and an
// unproved key is confirmed by the operator or refused — never assumed.
func (s Service) hostKey(ctx context.Context, contextName string, selected target,
	material map[string]secrets.Material, machines declaredTrust) (trust.HostKey, error) {
	if reference := selected.ssh.KnownHostsRef; reference != "" {
		return declaredHostKey(reference, selected, material)
	}
	if selected.installed {
		return s.installedHostKey(ctx, contextName, selected)
	}
	records, data, err := s.records(ctx, contextName, machines)
	if err != nil {
		return trust.HostKey{}, err
	}
	if record, found := records.Find(selected.name); found {
		return recordedHostKey(record, selected, contextName)
	}
	return s.firstUse(ctx, contextName, selected, records, data, machines)
}

// declaredHostKey binds the one entry the Machine's own Secret carries, so the
// target, port and key are fixed by desired state before anything is observed.
func declaredHostKey(reference string, selected target, material map[string]secrets.Material) (trust.HostKey, error) {
	bound, ok := material[reference]
	if !ok {
		return trust.HostKey{}, failure("access.unavailable",
			"the declared SSH host key Secret "+reference+" is not available", "")
	}
	value, ok := bound.Part(secrets.ValuePart)
	if !ok || len(value) == 0 {
		return trust.HostKey{}, failure("access.unavailable",
			"the declared SSH host key Secret "+reference+" carries no material", "")
	}
	defer clear(value)
	return trust.ParseKnownHostsLine(string(value), selected.address, selected.port)
}

// installedHostKey proves a Machine against the key its own installation
// delivered or captured. It requires that this context still owns that
// installation: a Machine it no longer owns proves nothing, and a host
// answering on the address would be trusted on first sight.
func (s Service) installedHostKey(ctx context.Context, contextName string, selected target) (trust.HostKey, error) {
	if s.options.Ownership == nil || s.options.Evidence == nil {
		return trust.HostKey{}, failure("access.unavailable",
			"installation evidence is unavailable for "+selected.object.Identity(), "")
	}
	owned, err := s.options.Ownership.Ownership(ctx, contextName)
	if err != nil {
		return trust.HostKey{}, err
	}
	if !owned["Machine/"+selected.name].Realized() {
		return trust.HostKey{}, failure("access.target",
			"this context has not installed "+selected.object.Identity()+", so its host key is unproved",
			"bootwright apply --context "+contextName+", or reach the Machine with its own declared access")
	}
	proved, found, err := s.options.Evidence.HostKey(ctx, contextName, selected.name)
	if err != nil {
		return trust.HostKey{}, err
	}
	if !found || !proved.Present() {
		return trust.HostKey{}, failure("access.target",
			"the installation of "+selected.object.Identity()+" proved no host key", "")
	}
	if proved.Address != selected.address {
		return trust.HostKey{}, failure("trust.identity",
			"the installation of "+selected.object.Identity()+" proved a host key for "+proved.Address+
				", not for "+selected.address,
			"declare an ssh address of "+proved.Address+" on "+selected.object.Identity()+
				", the address its installation proved, or remove the ssh address it declares")
	}
	return proved.HostKey, nil
}

// recordedHostKey pins what this context already trusts. A record for another
// endpoint is not a proof of this one, so the session refuses rather than
// pinning a key that was never confirmed for the address it is dialing.
func recordedHostKey(record trust.Record, selected target, contextName string) (trust.HostKey, error) {
	if record.Address != selected.address || record.Port != selected.port {
		return trust.HostKey{}, failure("trust.identity",
			"the trusted host key for "+selected.object.Identity()+" was recorded for "+
				trust.HostToken(record.Address, record.Port)+", not for "+
				trust.HostToken(selected.address, selected.port),
			"re-trust it with bootwright machine trust --context "+contextName+" --machines "+selected.name+
				" --replace "+selected.name)
	}
	return record.HostKey(), nil
}

// firstUse observes an unproved endpoint and asks the operator to accept what
// it presented. Nothing is recorded without that explicit confirmation, and
// without a terminal to ask on there is no first use at all. The record it
// would write replaces any record at the same endpoint of a Machine that no
// longer uses this context's trust, undeclared or exempt (D117), named before
// the prompt, and a write that would still pin the endpoint to two keys
// refuses before anything is asked.
func (s Service) firstUse(ctx context.Context, contextName string, selected target,
	records trust.Store, previous []byte, machines declaredTrust) (trust.HostKey, error) {
	remedy := "record it with bootwright machine trust --context " + contextName + " --machines " + selected.name
	interactive, err := s.interactive()
	if err != nil || !interactive {
		return trust.HostKey{}, failure("trust.identity",
			"no trusted SSH host key is held for "+selected.object.Identity(), remedy)
	}
	if s.options.Observer == nil || s.options.Confirmer == nil || s.options.Trust == nil {
		return trust.HostKey{}, failure("trust.identity",
			"no trusted SSH host key is held for "+selected.object.Identity(), remedy)
	}
	observed, err := s.options.Observer.Observe(ctx, selected.address, selected.port)
	if err != nil {
		return trust.HostKey{}, err
	}
	endpoint := trust.HostToken(selected.address, selected.port)
	record := trust.Record{
		Machine: selected.name, Address: selected.address, Port: selected.port,
		KeyType: observed.Type, PublicKey: observed.PublicKey, Fingerprint: observed.Fingerprint(),
		Source: trust.SourceFirstUse, Recorded: s.now(),
	}
	candidate := records.Clone()
	removed := candidate.Supersede(record, machines.uses)
	if err := candidate.Validate(); err != nil {
		return trust.HostKey{}, retrust(err, contextName, selected.name, machines)
	}
	for _, stale := range removed {
		why := "which it no longer declares"
		if exemption := machines.exempt(stale.Machine); exemption != "" {
			why = "which no longer uses this context's SSH trust (" + exemption + ")"
		}
		s.warn("trust.identity: confirming also removes the host key this context trusted for " + stale.Machine +
			", " + why + ", at " + endpoint)
	}
	if err := s.options.Confirmer.ConfirmHostKey(ctx, contextName, selected.name, endpoint,
		observed.Type, observed.Fingerprint()); err != nil {
		return trust.HostKey{}, err
	}
	data, err := candidate.Encode()
	if err != nil {
		return trust.HostKey{}, err
	}
	if err := s.options.Trust.ReplaceHostKeys(ctx, contextName, data, previous); err != nil {
		return trust.HostKey{}, err
	}
	return observed, nil
}

func (s Service) records(ctx context.Context, contextName string, machines declaredTrust) (trust.Store, []byte, error) {
	if s.options.Trust == nil {
		return trust.Store{FormatVersion: trust.FormatVersion}, nil, nil
	}
	data, err := s.options.Trust.ReadHostKeys(ctx, contextName)
	if err != nil {
		return trust.Store{}, nil, err
	}
	records, err := trust.Decode(data)
	if err != nil {
		return trust.Store{}, nil, retrust(err, contextName, "", machines)
	}
	return records, data, nil
}

// retrust names the re-trust that settles a pin a write would leave divergent,
// in this context: that of the Machine whose record the write did not make.
func retrust(err error, contextName, writing string, machines declaredTrust) error {
	var pin *trust.DivergentPin
	if errors.As(err, &pin) {
		return pin.Retrust(contextName, func(name string) bool { return name == writing }, machines.exempt)
	}
	return err
}

// references names every declaration this session needs opened: the credential
// it authenticates with and the host key it pins, and nothing else.
func references(selected target, privateKeyRef string) []string {
	var names []string
	if privateKeyRef != "" {
		names = append(names, privateKeyRef)
	}
	if selected.ssh.KnownHostsRef != "" {
		names = append(names, selected.ssh.KnownHostsRef)
	}
	return names
}

// privateKey takes the bound half the session authenticates with. The bytes
// stay in the caller's memory and reach the client as a descriptor alone.
func privateKey(reference string, material map[string]secrets.Material) ([]byte, error) {
	bound, ok := material[reference]
	if !ok {
		return nil, failure("access.unavailable", "the SSH private key Secret "+reference+" is not available", "")
	}
	value, ok := bound.Part(secrets.PrivateKeyPart)
	if !ok || len(value) == 0 {
		return nil, failure("access.unavailable", "the SSH private key Secret "+reference+" carries no private half", "")
	}
	return value, nil
}

// passwordAdvisory names the reveal an operator performs themselves when the
// client is going to ask for a password this context holds. The product never
// answers the prompt on their behalf.
func passwordAdvisory(selected target, contextName string) string {
	if selected.ssh.PasswordRef == "" {
		return ""
	}
	return "this Machine authenticates with the " + selected.ssh.PasswordRef +
		" Secret; the client will ask for it, and bootwright secret show --context " + contextName + " --name " +
		selected.ssh.PasswordRef + " --part password reveals it"
}
