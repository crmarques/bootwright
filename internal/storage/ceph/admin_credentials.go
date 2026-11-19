package ceph

import (
	"context"
	"encoding/json"
	"time"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const AdminPasswordOutput = "admin-password"

const (
	adminCredentialDeadline = 3 * time.Minute
	monitoringLoginDeadline = 6 * time.Minute
	grafanaLoginDeadline    = 5 * time.Minute
	loginPhase              = "login_update"
	GrafanaLogin            = "grafana"
	GrafanaUser             = "admin"
)

func MonitoringBlock(cluster string) string {
	return ServiceUnitBlockID(cluster, ServiceUnit{Type: "monitoring"})
}

func (c Capability) AdminLogins(block reconciliation.Block) ([]lifecycle.AdminLogin, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	if request.Action == "cluster" {
		return []lifecycle.AdminLogin{{Name: "dashboard", User: DashboardUser, Output: AdminPasswordOutput, Seed: DashboardOutput, Custody: lifecycle.AdminProducedPassword}}, nil
	}
	var logins []lifecycle.AdminLogin
	for _, service := range credentialServices(request) {
		logins = append(logins, lifecycle.AdminLogin{Name: service, Output: MonitoringOutput, Custody: lifecycle.AdminProducedLogin})
	}
	if secret, bound := grafanaBinding(request); bound {
		logins = append(logins, lifecycle.AdminLogin{Name: GrafanaLogin, User: GrafanaUser, Secret: secret, Custody: lifecycle.AdminBoundSecret})
	}
	return logins, nil
}

func grafanaBinding(r Request) (string, bool) {
	if r.Action != "service" || r.Service == nil || r.Service.Type != "monitoring" {
		return "", false
	}
	for _, binding := range r.Service.Bindings {
		if binding.ServiceRef == "grafana" && binding.Path == "/spec/initial_admin_password" {
			return binding.SecretRef, true
		}
	}
	return "", false
}

func (c Capability) PrepareAdminCredential(ctx context.Context, e lifecycle.Execution, login lifecycle.AdminLogin) (secrets.Material, error) {
	return lifecycle.PrepareAdminCredential(ctx, c.adminCredentialCall(login), e)
}

func (c Capability) UpdateAdminCredential(ctx context.Context, e lifecycle.Execution, login lifecycle.AdminLogin, update secrets.Material) error {
	return lifecycle.UpdateAdminCredential(ctx, c.adminCredentialCall(login), e, update)
}

func (c Capability) adminCredentialCall(login lifecycle.AdminLogin) lifecycle.AdminCredentialCall {
	guard := loginGuard
	if login.Custody == lifecycle.AdminProducedPassword {
		guard = dashboardGuard
	}
	return lifecycle.AdminCredentialCall{Runner: c.runner, Adapter: "Ceph", Bind: loginBinder{capability: c, login: login}.bind, Guard: guard,
		Missing: failure("access.unavailable", "Ceph returned no administrator credential identity", "check the original "+login.Name+" administrator login")}
}

type loginBinder struct {
	capability Capability
	login      lifecycle.AdminLogin
}

func (b loginBinder) bind(_ context.Context, e lifecycle.Execution) (lifecycle.Execution, lifecycle.Invocation, func(), error) {
	request, err := DecodeRequest(e.Block.Request)
	if err != nil {
		return e, lifecycle.Invocation{}, nil, err
	}
	proof, err := decodeEvidence(e.PreviousEvidence)
	if err != nil || !validIncarnation(proof.Incarnation) {
		return e, lifecycle.Invocation{}, nil, evidenceFailure()
	}
	phase, deadline := "observe", adminCredentialDeadline
	if b.login.Custody != lifecycle.AdminProducedPassword {
		phase = loginPhase
	}
	switch b.login.Custody {
	case lifecycle.AdminProducedLogin:
		deadline = monitoringLoginDeadline
	case lifecycle.AdminBoundSecret:
		deadline = grafanaLoginDeadline
	}
	execution, files, cleanup, err := boundMaterial(e, request, phase, b.capability.proofs)
	if err != nil {
		return e, lifecycle.Invocation{}, nil, err
	}
	return execution, lifecycle.Invocation{Implementation: Implementation, Variable: "bootwright_ceph", Canonical: e.Block.Request,
		Placement: machine.Placement{Connection: machine.ConnectionLocal, Machine: request.Controller}, Materials: files, Values: map[string]string{"incarnation": proof.Incarnation, "login": b.login.Name},
		Refusals: loginRefusals(), Deadline: deadline}, cleanup, nil
}

func dashboardGuard(guard secrets.Material) (secrets.Material, error) {
	value, _ := guard.Part(secrets.ValuePart)
	defer clear(value)
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: value}), nil
}

func loginGuard(prepared secrets.Material) (secrets.Material, error) {
	value, _ := prepared.Part(secrets.ValuePart)
	defer clear(value)
	var decoded struct {
		Guard json.RawMessage `json:"guard"`
	}
	if len(value) > 8192 || canonicaljson.DecodeClosed(value, &decoded) != nil || len(decoded.Guard) == 0 || len(decoded.Guard) > 4096 || !json.Valid(decoded.Guard) || decoded.Guard[0] != '{' {
		return secrets.Material{}, failure("access.unavailable", "the Ceph login preparation returned no valid guard", "repeat the credential update")
	}
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: decoded.Guard}), nil
}

func (c Capability) AdminCredentialIdentity(_ context.Context, e lifecycle.Execution, login lifecycle.AdminLogin) (string, error) {
	request, err := DecodeRequest(e.Block.Request)
	if err != nil {
		return "", err
	}
	proof, err := decodeEvidence(e.PreviousEvidence)
	if err != nil || !validIncarnation(proof.Incarnation) {
		return "", evidenceFailure()
	}
	principal := "dashboard user " + DashboardUser
	switch login.Custody {
	case lifecycle.AdminProducedLogin:
		principal = "monitoring basic auth"
	case lifecycle.AdminBoundSecret:
		principal = "grafana admin"
	}
	return "cluster fsid " + request.FSID + " created " + proof.Incarnation + " as " + principal, nil
}

func (c Capability) AdminCredentialMayBeMissing(lifecycle.AdminLogin) bool { return false }

func (c Capability) GenerateAdminCredential(_ context.Context, login lifecycle.AdminLogin, entropy reconciliation.Entropy) (secrets.Material, error) {
	if login.Custody != lifecycle.AdminProducedLogin || entropy == nil {
		return secrets.Material{}, cephRequestInvalid.Fail("only a monitoring login's password is generated by the controller", "")
	}
	password := make([]byte, 0, dashboardLength)
	buffer := make([]byte, 64)
	defer clear(buffer)
	limit := 256 - 256%len(dashboardAlphabet)
	for len(password) < dashboardLength {
		if read, err := entropy(buffer); err != nil || read != len(buffer) {
			clear(password)
			return secrets.Material{}, failure("secret.input", "password generation failed", "repeat the credential update")
		}
		for _, b := range buffer {
			if int(b) < limit && len(password) < dashboardLength {
				password = append(password, dashboardAlphabet[int(b)%len(dashboardAlphabet)])
			}
		}
	}
	defer clear(password)
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: password}), nil
}
