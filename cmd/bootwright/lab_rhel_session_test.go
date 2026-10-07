//go:build linux && amd64

package main

import (
	"context"
	"io"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
)

// labSessionState renders the compiled example as the selected context's
// effective graph.
type labSessionState struct{ effective api.Catalog }

func (s labSessionState) RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	return &compilation.EffectiveResult{Effective: s.effective}, nil
}

// labSessionLender lends exactly the Secrets a session names, from the ones
// the context holds, and records what it was asked for.
type labSessionLender struct {
	held      map[string]secrets.Material
	requested []string
}

func (l *labSessionLender) WithMaterial(ctx context.Context, request lifecycle.MaterialRequest,
	use func(context.Context, map[string]secrets.Material) error) error {
	l.requested = append([]string(nil), request.Secrets...)
	lent := map[string]secrets.Material{}
	for _, name := range request.Secrets {
		if material, ok := l.held[name]; ok {
			lent[name] = material
		}
	}
	return use(ctx, lent)
}

// labSessionOwnership answers as a current apply that completed rhel-01.
type labSessionOwnership struct{}

func (labSessionOwnership) Ownership(context.Context, string) (map[string]machine.OwnershipState, error) {
	return map[string]machine.OwnershipState{"Machine/rhel-01": {Verb: machine.VerbApply, State: machine.BlockDone}}, nil
}

// labSessionProof answers with the host key the installation proved, at the
// address it proved it.
type labSessionProof struct{ evidence machine.HostKeyEvidence }

func (p labSessionProof) HostKey(context.Context, string, string) (machine.HostKeyEvidence, bool, error) {
	return p.evidence, true, nil
}

// labSessionClient records each session it would open and the credential that
// session carried while it ran, which is cleared once it ends.
type labSessionClient struct {
	sessions []machine.Session
	keys     []string
}

func (c *labSessionClient) Run(_ context.Context, session machine.Session, _ io.Reader, _, _ io.Writer) (int, error) {
	c.sessions = append(c.sessions, session)
	c.keys = append(c.keys, string(session.PrivateKey))
	return 0, nil
}

func (c *labSessionClient) IdentityFile(context.Context, string) (string, error) { return "", nil }

// The guest lab-rhel installs declares no ssh address, and its installation
// proves the host key at the address the installation request carries. A
// session therefore dials that address, so machine exec opens right after the
// apply with the fleet key and the proved host key, and no trust prompt.
func TestLabRHELInstalledGuestSessionDialsTheInstallAddress(t *testing.T) {
	state, _ := compileAcceptance(t, labExampleSources(t))
	capability, ok := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{}).Resolve(installation.Kind, installation.Implementation)
	if !ok {
		t.Fatal("the installation capability does not resolve")
	}
	contribution, err := capability.Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, State: state, Controller: "controller",
		Context: lifecycle.ContextIdentity{Name: "lab-rhel"},
	})
	if err != nil || len(contribution.Definitions) != 1 || contribution.Definitions[0].Object != "rhel-01" {
		t.Fatalf("the installation planned %+v (%v)", contribution.Definitions, err)
	}
	request, err := installation.DecodeRequest(contribution.Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	guest := requireObject(t, state.Effective(), api.Machine, "rhel-01")
	target, ok := machine.SSH(guest)
	if !ok || target.Address != request.Address || request.Address != "198.51.100.11" || target.Port != 22 {
		t.Fatalf("rhel-01 dials %+v, its installation proves %q", target, request.Address)
	}
	if target.User != "bootwright" || target.PrivateKeyRef == "" || target.KnownHostsRef != "" {
		t.Fatalf("rhel-01 access = %+v", target)
	}
	proved, err := trust.ParseAuthorizedKey("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICG+RadYRVwHqO7OQsOpyh8SDf1tbTQ8PUmgn7UEmou/")
	if err != nil {
		t.Fatal(err)
	}
	lender := &labSessionLender{held: map[string]secrets.Material{
		target.PrivateKeyRef: secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: []byte("FLEET KEY")}),
	}}
	client := &labSessionClient{}
	service := machineaccess.New(labSessionState{effective: state.Effective()}, nil, machineaccess.Options{
		Lender: lender, Ownership: labSessionOwnership{},
		Evidence: labSessionProof{evidence: machine.HostKeyEvidence{Address: request.Address, HostKey: proved}},
		Launcher: client,
	})
	result, err := service.Exec(context.Background(), machineaccess.ExecRequest{ContextName: "lab-rhel", Name: "rhel-01", Command: []string{"true"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Address != request.Address || len(client.sessions) != 1 {
		t.Fatalf("result = %+v, sessions = %+v", result, client.sessions)
	}
	session := client.sessions[0]
	if session.Address != request.Address || session.User != "bootwright" || session.Kind != machine.IdentityKey {
		t.Fatalf("session = %+v", session)
	}
	if session.HostKey.Type != proved.Type || session.HostKey.PublicKey != proved.PublicKey {
		t.Fatalf("pinned %+v, want the installed key %+v", session.HostKey, proved)
	}
	if client.keys[0] != "FLEET KEY" || len(lender.requested) != 1 || lender.requested[0] != target.PrivateKeyRef {
		t.Fatalf("the session carried %q after opening %v", client.keys[0], lender.requested)
	}
}
