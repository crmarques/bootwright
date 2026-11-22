package enrollment

import (
	"context"
	"slices"
	"strconv"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/trust"
)

func (s Service) enrollReferenced(ctx context.Context, request EnrollRequest) (*Report, error) {
	if s.options.Pins == nil || s.options.Observer == nil {
		return nil, availability.ErrNotImplemented
	}
	if len(request.Machines) != 1 {
		return nil, failure("trust.identity", "referenced Secret enrollment needs one Machine", "select one Machine with --name")
	}
	name := request.Machines[0]
	for machineName := range request.Expect {
		if machineName != name {
			return nil, failure("trust.identity", "an expectation names a different Machine", "name the selected Machine in --expect")
		}
	}
	selected, err := machine.SelectedContext(ctx, s.selection, request.ContextName)
	if err != nil {
		return nil, err
	}
	var endpoint machine.SSHTarget
	var reference string
	pin, err := s.options.Pins(ctx, selected, func(catalog api.Catalog) (string, error) {
		endpoint, reference, err = referencedTarget(catalog, name)
		return reference, err
	})
	defer clear(pin.Value)
	if err != nil {
		return nil, err
	}
	expectation := request.Expect[name]
	key, err := s.observeReferenced(ctx, name, endpoint, request.HostPublicKeyFile, expectation)
	if err != nil {
		return nil, err
	}
	remedy := "bootwright machine trust --context " + selected + " --name " + name + " --referenced-secret"
	row, err := comparePin(name, reference, endpoint, key, string(pin.Value), pin.Version, slices.Contains(request.Replace, name), remedy)
	if err != nil {
		return nil, err
	}
	report := &Report{Context: selected, DryRun: request.DryRun, Hosts: []HostReport{row}}
	if row.Action == ActionReuse {
		return report, nil
	}
	report.Pending = 1
	if err := pin.CheckWrite(ctx); err != nil {
		return nil, err
	}
	if request.DryRun {
		return report, nil
	}
	if request.SkipConfirmation && expectation == "" && request.HostPublicKeyFile == "" {
		return nil, failure("trust.identity", "unattended enrollment requires an independently verified key for Machine/"+name,
			"repeat "+remedy+" with --expect or --host-public-key-file")
	}
	if err := s.confirm(ctx, selected, request, report); err != nil {
		return nil, err
	}
	version, err := pin.Publish(ctx, key.Line(endpoint.Address, endpoint.Port))
	if err != nil {
		return nil, err
	}
	report.Recorded, report.Hosts[0].Version = 1, version
	return report, nil
}

func (s Service) observeReferenced(ctx context.Context, name string, endpoint machine.SSHTarget, path, expected string) (trust.HostKey, error) {
	var key trust.HostKey
	var err error
	if path == "" {
		key, err = s.options.Observer.Observe(ctx, endpoint.Address, endpoint.Port)
	} else {
		observer, ok := s.options.Observer.(KeyObserver)
		if !ok || s.options.PublicKey == nil {
			return trust.HostKey{}, availability.ErrNotImplemented
		}
		value, readErr := s.options.PublicKey(ctx, path)
		defer clear(value)
		if readErr != nil {
			return trust.HostKey{}, readErr
		}
		input, parseErr := trust.ParseAuthorizedKey(string(value))
		if parseErr != nil {
			return trust.HostKey{}, parseErr
		}
		if err := input.Validate(); err != nil {
			return trust.HostKey{}, err
		}
		if expected != "" && expected != input.Fingerprint() {
			return trust.HostKey{}, failure("trust.identity", "the public-key file and expected fingerprint disagree for Machine/"+name, "verify both inputs out of band")
		}
		key, err = observer.ObserveKey(ctx, name, endpoint.Address, endpoint.Port, input)
		expected = input.Fingerprint()
	}
	return checkObservation(key, err, name, endpoint, expected)
}

func checkObservation(key trust.HostKey, err error, name string, endpoint machine.SSHTarget, expected string) (trust.HostKey, error) {
	if err != nil {
		return trust.HostKey{}, err
	}
	if err := key.Validate(); err != nil {
		return trust.HostKey{}, err
	}
	if expected != "" && key.Fingerprint() != expected {
		return trust.HostKey{}, failure("trust.identity",
			"Machine/"+name+" at "+trust.HostToken(endpoint.Address, endpoint.Port)+" presented "+key.Type+" "+key.Fingerprint()+"; expected "+expected,
			"verify the fingerprint independently or supply the intended server public key with --host-public-key-file")
	}
	return key, nil
}

func comparePin(name, reference string, endpoint machine.SSHTarget, key trust.HostKey, previous, version string, replace bool, command string) (HostReport, error) {
	row := HostReport{Machine: name, Secret: reference, Address: endpoint.Address, Port: endpoint.Port,
		Action: ActionAdd, KeyType: key.Type, Fingerprint: key.Fingerprint()}
	if version == "" {
		return row, nil
	}
	old, err := trust.ParseKnownHostsLine(previous, endpoint.Address, endpoint.Port)
	if err == nil && old.Validate() == nil {
		row.PreviousFingerprint = old.Fingerprint()
		if row.PreviousFingerprint == row.Fingerprint {
			row.Action, row.Version = ActionReuse, version
			return row, nil
		}
	} else {
		row.Reason = "the previous pin is invalid or belongs to another endpoint"
	}
	if !replace {
		detail := row.Reason
		if detail == "" {
			detail = "the key changed from " + row.PreviousFingerprint + " to " + row.Fingerprint
		}
		return HostReport{}, failure("trust.identity",
			"Machine/"+name+" at "+trust.HostToken(endpoint.Address, endpoint.Port)+" uses Secret/"+reference+"; "+detail,
			"verify the expected key out of band, then repeat "+command+" --replace "+name)
	}
	row.Action = ActionReplace
	return row, nil
}

func referencedTarget(catalog api.Catalog, name string) (machine.SSHTarget, string, error) {
	object, found := catalog.Find(api.Machine, name)
	ssh, remote := machine.SSH(object)
	if !found || !remote || !api.MachineProvided(object) || machine.ReachedLocally(object) {
		return machine.SSHTarget{}, "", failure("trust.identity", "Machine/"+name+" does not resolve to a remote provided SSH endpoint", "select a provided Machine with SSH access")
	}
	spec := object.Spec()
	lifecycle := spec.Get("access", "lifecycle", "knownHostsRef").Text()
	ref := ssh.KnownHostsRef
	if ref == "" || lifecycle != "" && lifecycle != ref {
		return machine.SSHTarget{}, "", failure("trust.identity",
			"Machine/"+name+" does not name a single shared SSH host-key Secret (access.ssh "+ref+", access.lifecycle "+lifecycle+")",
			"set access.ssh.knownHostsRef and any dedicated lifecycle knownHostsRef to the same Secret")
	}
	for _, consumer := range catalog.Objects() {
		if consumer.Identity() == object.Identity() {
			continue
		}
		path := otherSecretUse(consumer.Spec(), api.Schema(consumer.Kind()), ref, "spec")
		if path != "" {
			return machine.SSHTarget{}, "", failure("trust.identity", "Secret/"+ref+" is also referenced by "+consumer.Identity()+" at "+path,
				"give Machine/"+name+" its own knownHostsRef before enrolling it")
		}
	}
	return ssh, ref, nil
}

func otherSecretUse(value api.Value, shape *api.Shape, ref, path string) string {
	if shape == nil || !value.Present() {
		return ""
	}
	if value.Text() == ref && slices.Contains(shape.Reference, api.Secret) {
		return path
	}
	for _, alternate := range shape.Alternatives {
		if found := otherSecretUse(value, alternate, ref, path); found != "" {
			return found
		}
	}
	for _, field := range value.Fields() {
		child, _ := shape.Field(field.Name)
		next := child.Shape
		if shape.Open {
			next = shape.Element
		}
		if found := otherSecretUse(field.Value, next, ref, path+"."+field.Name); found != "" {
			return found
		}
	}
	for i, item := range value.Items() {
		if found := otherSecretUse(item, shape.Element, ref, path+"["+strconv.Itoa(i)+"]"); found != "" {
			return found
		}
	}
	return ""
}
