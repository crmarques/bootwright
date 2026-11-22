package account

import (
	"context"
	_ "embed"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
)

//go:embed account.py
var worker string

func validUser(user string) bool { return api.ValidLexical(api.LexicalPOSIXUser, user) }

func (r Remote) HardwareAddresses(ctx context.Context, contextName string, place machine.Placement, access machine.Bootstrap, material map[string]secrets.Material) ([]string, error) {
	if r.Hardware == "" {
		return nil, refused(place, "hardware identity", "use a build with the Machine hardware reader")
	}
	output, err := r.bootstrap(ctx, contextName, place, access, material, false,
		[]string{"/usr/bin/python3", "-I", "-c", r.Hardware + "\nimport json\nprint(json.dumps(read()))"})
	if err != nil {
		return nil, err
	}
	addresses, err := reconciliation.DecodeEvidence[[]string](output, 32<<10, "machine hardware")
	if err != nil || addresses == nil {
		return nil, refused(place, "hardware identity", "check that the bootstrap account can read the permanent network addresses")
	}
	return addresses, nil
}

func (r Remote) manage(ctx context.Context, contextName string, request Request, owner, mode string, material map[string]secrets.Material) (string, error) {
	workerMode := mode
	if mode == "inspect" {
		workerMode = "observe"
	}
	args := []string{"/usr/bin/timeout", "--kill-after=5s", "55s", "/usr/bin/python3", "-I", "-c", worker, workerMode, request.Placement.User, owner}
	keys, err := publicKeys(request, material)
	if err != nil {
		return "", err
	}
	args = append(args, keys...)
	var output []byte
	if mode == "inspect" {
		output, err = r.asManaged(ctx, contextName, request.Placement, material, append([]string{sudoExecutable, "-H", "-n", "-k", "-u", "root", "--"}, args...))
	} else {
		output, err = r.bootstrap(ctx, contextName, request.Placement, request.Bootstrap, material, true, args)
	}
	if err != nil {
		return "", err
	}
	state := strings.TrimSpace(string(output))
	switch state {
	case "ready", "absent", "partial":
		return state, nil
	}
	return "", refused(request.Placement, "the account state", "verify the remote administrator and retry")
}

func (r Remote) prove(ctx context.Context, contextName string, request Request, material map[string]secrets.Material) error {
	place := request.Placement
	host, err := machine.BoundHostKey(contextName, place.Machine, place.Address, place.Port, place.KnownHostsRef, material)
	if err != nil {
		return err
	}
	for _, name := range request.keys() {
		key, _ := material[name].Part(secrets.PrivateKeyPart)
		session := machine.Session{Machine: place.Machine, Address: place.Address, Port: place.Port, HostKey: host, Batch: true,
			Identity: machine.Identity{User: place.User, Kind: machine.IdentityKey, PrivateKey: key}}
		session.Command = []string{"/usr/bin/id", "-un"}
		user, loginErr := r.run(ctx, session, nil)
		if loginErr != nil || strings.TrimSpace(string(user)) != place.User {
			clear(key)
			return refused(place, "the new SSH login as "+place.User, "check the provisioned account and its SSH authorization")
		}
		session.Command = []string{sudoExecutable, "-H", "-n", "-k", "-u", "root", "--", "/usr/bin/id", "-u"}
		uid, sudoErr := r.run(ctx, session, nil)
		clear(key)
		if sudoErr != nil || strings.TrimSpace(string(uid)) != "0" {
			return remoteRefusal(sudoErr, place, place.User, "noninteractive root sudo as "+place.User, "check the provisioned account and its remote sudo policy")
		}
	}
	return nil
}

func publicKeys(request Request, material map[string]secrets.Material) ([]string, error) {
	names := request.keys()
	if request.ClusterKeyRef != "" {
		names = append(names, request.ClusterKeyRef)
	}
	var distinct []string
	for _, name := range names {
		public, _ := material[name].Part(secrets.PublicKeyPart)
		key, err := trust.ParseAuthorizedKey(string(public))
		if err != nil {
			return nil, refused(request.Placement, "the managed public keys", "provide supported SSH key pairs")
		}
		line := key.Type + " " + key.PublicKey
		if slices.Contains(distinct, line) {
			return nil, refused(request.Placement, "distinct lifecycle, fleet and native cluster keys", "bind independent SSH key pairs")
		}
		distinct = append(distinct, line)
	}
	return distinct[:len(request.keys())], nil
}

func (r Remote) asManaged(ctx context.Context, contextName string, place machine.Placement, material map[string]secrets.Material, args []string) ([]byte, error) {
	host, err := machine.BoundHostKey(contextName, place.Machine, place.Address, place.Port, place.KnownHostsRef, material)
	if err != nil {
		return nil, err
	}
	key, _ := material[place.PrivateKeyRef].Part(secrets.PrivateKeyPart)
	defer clear(key)
	session := machine.Session{Machine: place.Machine, Address: place.Address, Port: place.Port, HostKey: host, Batch: true, Command: args,
		Identity: machine.Identity{User: place.User, Kind: machine.IdentityKey, PrivateKey: key}}
	output, err := r.run(ctx, session, nil)
	if err != nil {
		return nil, remoteRefusal(err, place, place.User, "the managed login and sudo", "check the managed account's granted SSH key and sudo policy")
	}
	return output, nil
}
