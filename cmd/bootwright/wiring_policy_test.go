//go:build linux && amd64

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/privilege"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	environmentaccess "github.com/crmarques/bootwright/internal/environment/access"
	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/sessionpolicy"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

type policyRegistry func(context.Context) (contexts.Registry, error)

func (view policyRegistry) View(ctx context.Context) (contexts.Registry, error) {
	return view(ctx)
}

type policyGraph func(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error)

func (render policyGraph) RenderEffective(ctx context.Context, request compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	return render(ctx, request)
}

type policyAccount func(context.Context) (privilege.Account, error)

func (resolve policyAccount) Resolve(ctx context.Context) (privilege.Account, error) {
	return resolve(ctx)
}

type policyStore func(context.Context, string) ([]byte, error)

func (read policyStore) ReadSessionPolicy(ctx context.Context, name string) ([]byte, error) {
	return read(ctx, name)
}

const sessionPolicyYAML = `apiVersion: bootwright.io/v1alpha1
kind: SessionAccessPolicy
metadata:
  name: lab

spec:
  grants:
    - name: diagnostics
      remoteUser: operator

      subjects:
        - group: support

      targets:
        machineRefs:
          - node

      exec:
        commands:
          - - /usr/bin/uptime
`

func testPolicyGraph(t *testing.T, renderErr error) effectiveGraph {
	t.Helper()
	catalog := api.NewCatalog([]api.Object{api.NewObject(api.Machine, "node", api.MapValue(), api.MapValue())})
	return effectiveGraph{state: policyGraph(func(_ context.Context, request compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
		if request.ContextName != "lab" {
			t.Fatalf("rendered context %q, want lab", request.ContextName)
		}
		return &compilation.EffectiveResult{Effective: catalog}, renderErr
	})}
}

func lookupPolicyGroup(t *testing.T) func(context.Context, bool, string) (uint32, error) {
	t.Helper()
	return func(_ context.Context, group bool, name string) (uint32, error) {
		if !group || name != "support" {
			t.Fatalf("looked up %q (group %t), want support group", name, group)
		}
		return 2001, nil
	}
}

func TestPolicyInstallPublishesOnlyValidatedBytesForTheSelectedRecord(t *testing.T) {
	failure := errors.New("storage failed")
	selected := contexts.Record{Name: "lab", Revision: "test-revision"}
	for _, test := range []struct {
		name              string
		data              string
		registry          contexts.Registry
		view, render      error
		publish, want     error
		wantPublish       bool
		wantPolicyRefusal bool
	}{
		{name: "success", data: sessionPolicyYAML, wantPublish: true},
		{name: "publication error", data: sessionPolicyYAML, publish: failure, want: failure, wantPublish: true},
		{name: "registry error", view: failure, want: failure},
		{name: "unknown context", registry: contexts.Registry{Contexts: []contexts.Record{{Name: "other"}}}},
		{name: "render error", render: failure, want: failure},
		{name: "missing input"},
		{name: "invalid policy", data: "invalid", wantPolicyRefusal: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "policy.yaml")
			if test.data != "" {
				if err := os.WriteFile(file, []byte(test.data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			registry := test.registry
			if registry.Contexts == nil {
				registry.Contexts = []contexts.Record{{Name: "other"}, selected}
			}
			published := false
			commands := policyCommands{
				repository: policyRegistry(func(context.Context) (contexts.Registry, error) { return registry, test.view }),
				graph:      testPolicyGraph(t, test.render),
				service:    sessionpolicy.Service{Lookup: lookupPolicyGroup(t)},
				publish: func(_ context.Context, record contexts.Record, data []byte) error {
					published = true
					if record != selected || string(data) != test.data {
						t.Fatalf("published record %+v with data %q", record, data)
					}
					return test.publish
				},
			}
			name, err := commands.Install(t.Context(), "lab", file)
			if published != test.wantPublish || (name == "lab") != test.wantPublish {
				t.Fatalf("context %q, published %t, error %v", name, published, err)
			}
			switch {
			case test.want != nil || test.wantPublish:
				if !errors.Is(err, test.want) {
					t.Fatalf("got %v, want %v", err, test.want)
				}
			case test.wantPolicyRefusal:
				requirePolicyDiagnostic(t, err, "access.policy")
			default:
				if err == nil {
					t.Fatal("install accepted an absent context or input file")
				}
			}
		})
	}
}

func TestPolicyPermissionsUsesTheVerifiedCallerAndReleasesAdmission(t *testing.T) {
	failure := errors.New("port failed")
	for _, test := range []struct {
		name                          string
		selection, account, hold      error
		render                        error
		uid, parent                   int
		delegated, wantHold, wantRead bool
		want                          error
	}{
		{name: "verified group grant", uid: 1001, parent: 42, delegated: true, wantHold: true, wantRead: true},
		{name: "administrative permissions", wantHold: true, wantRead: true},
		{name: "selection error", selection: failure, want: failure},
		{name: "account lookup error", account: failure},
		{name: "delegated caller without verified parent", uid: 1001, delegated: true},
		{name: "root cannot be a delegated caller", parent: 42, delegated: true},
		{name: "reservation error", hold: failure, want: failure, wantHold: true},
		{name: "graph error", render: failure, want: failure, wantHold: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			held, released, read := false, false, false
			commands := policyCommands{
				delegated: test.delegated,
				account: policyAccount(func(context.Context) (privilege.Account, error) {
					return privilege.Account{UID: test.uid, SudoParentPID: test.parent, Groups: []uint32{2001}}, test.account
				}),
				selection: func(context.Context) (string, error) { return "lab", test.selection },
				hold: func(context.Context) (func(), error) {
					held = true
					return func() { released = true }, test.hold
				},
				graph: testPolicyGraph(t, test.render),
				service: sessionpolicy.Service{
					Lookup: lookupPolicyGroup(t),
					Store: policyStore(func(_ context.Context, name string) ([]byte, error) {
						read = true
						if name != "lab" || !held || released {
							t.Fatalf("policy read for %q with held=%t released=%t", name, held, released)
						}
						return []byte(sessionPolicyYAML), nil
					}),
				},
			}
			name, grants, err := commands.Permissions(t.Context(), "")
			if held != test.wantHold || released != (test.wantHold && test.hold == nil) || read != test.wantRead {
				t.Fatalf("held=%t released=%t read=%t, error %v", held, released, read, err)
			}
			switch {
			case test.wantRead:
				if err != nil || name != "lab" || len(grants) != 1 || !slices.Equal(grants[0].Machines, []string{"node"}) {
					t.Fatalf("permissions for %q: %+v, %v", name, grants, err)
				}
			case test.want != nil:
				if !errors.Is(err, test.want) {
					t.Fatalf("got %v, want %v", err, test.want)
				}
			default:
				requirePolicyDiagnostic(t, err, "access.denied")
			}
		})
	}
}

func requirePolicyDiagnostic(t *testing.T, err error, code string) {
	t.Helper()
	if got := diagnostics.Of(err); len(got) != 1 || got[0].Code != code {
		t.Fatalf("got diagnostics %+v, want %s", got, code)
	}
}

func TestPolicyAuthorizationUsesOneEffectiveRosterForTheVerifiedCaller(t *testing.T) {
	nodes := api.ListValue(api.MapValue().With("machineRef", api.StringValue("node")))
	catalog := api.NewCatalog([]api.Object{
		api.NewObject(api.Machine, "node", api.MapValue(), api.MapValue()),
		api.NewObject(api.OpenShiftCluster, "apps", api.MapValue(), api.MapValue().With("nodes", nodes)),
	})
	commands := policyCommands{
		delegated: true,
		account: policyAccount(func(context.Context) (privilege.Account, error) {
			return privilege.Account{UID: 1001, SudoParentPID: 42, Groups: []uint32{2001}}, nil
		}),
		service: sessionpolicy.Service{
			Members: policyRoster(),
			Lookup:  lookupPolicyGroup(t),
			Store: policyStore(func(_ context.Context, name string) ([]byte, error) {
				if name != "lab" {
					t.Fatalf("read policy for %q, want lab", name)
				}
				data := strings.Replace(sessionPolicyYAML, "machineRefs:\n          - node", "clusterRefs:\n          - apps", 1)
				return []byte(data), nil
			}),
		},
	}
	targets := []machine.SessionTarget{{Machine: "node", RemoteUser: "operator"}}
	argv := []string{"/usr/bin/uptime"}
	if err := commands.authorize(t.Context(), "lab", catalog, targets, argv); err != nil {
		t.Fatalf("cluster roster did not authorize the node: %+v", diagnostics.Of(err))
	}
	targets[0].RemoteUser = "different"
	requirePolicyDiagnostic(t, commands.authorize(t.Context(), "lab", catalog, targets, argv), "access.denied")
	commands.account = boundaryAccounts{err: errors.New("account failed")}
	requirePolicyDiagnostic(t, commands.authorize(t.Context(), "lab", catalog, targets, argv), "access.denied")
}

type policySessionPort func(context.Context, string, string, []string) (*machine.SessionResult, error)

func (run policySessionPort) Exec(ctx context.Context, request machineaccess.ExecRequest) (*machine.SessionResult, error) {
	return run(ctx, "machine exec", request.ContextName, request.Command)
}

func (run policySessionPort) Rsh(ctx context.Context, request machineaccess.RshRequest) (*machine.SessionResult, error) {
	return run(ctx, "machine rsh", request.ContextName, nil)
}

func (run policySessionPort) ClusterExec(ctx context.Context, request environmentaccess.ClusterExecRequest) (*machine.SessionResult, error) {
	return run(ctx, "cluster exec", request.ContextName, request.Command)
}

func (run policySessionPort) ClusterRsh(ctx context.Context, request environmentaccess.ClusterRshRequest) (*machine.SessionResult, error) {
	return run(ctx, "cluster rsh", request.ContextName, nil)
}

func TestDelegatedSessionsReleaseAdmissionAtPreparationOrRefusal(t *testing.T) {
	command := []string{"/usr/bin/uptime"}
	refused := errors.New("session refused")
	for _, test := range []struct {
		name    string
		command []string
		invoke  func(delegatedSessions) (*machine.SessionResult, error)
	}{
		{"machine exec", command, func(d delegatedSessions) (*machine.SessionResult, error) {
			return d.Exec(t.Context(), machineaccess.ExecRequest{Command: command})
		}},
		{"machine rsh", nil, func(d delegatedSessions) (*machine.SessionResult, error) {
			return d.Rsh(t.Context(), machineaccess.RshRequest{ContextName: "lab"})
		}},
		{"cluster exec", command, func(d delegatedSessions) (*machine.SessionResult, error) {
			return d.ClusterExec(t.Context(), environmentaccess.ClusterExecRequest{Command: command})
		}},
		{"cluster rsh", nil, func(d delegatedSessions) (*machine.SessionResult, error) {
			return d.ClusterRsh(t.Context(), environmentaccess.ClusterRshRequest{ContextName: "lab"})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, ready := range []bool{true, false} {
				held := false
				port := policySessionPort(func(ctx context.Context, route, selected string, argv []string) (*machine.SessionResult, error) {
					if !held || route != test.name || selected != "lab" || !slices.Equal(argv, test.command) {
						t.Fatalf("admission held=%t for %s in %s with %v", held, route, selected, argv)
					}
					if !ready {
						return nil, refused
					}
					sessionPrepared(ctx)
					if held {
						t.Fatal("session still holds admission after preparation")
					}
					return &machine.SessionResult{Context: selected}, nil
				})
				d := delegatedSessions{
					selection: func(context.Context) (string, error) { return "lab", nil },
					hold: func(context.Context) (func(), error) {
						held = true
						return sync.OnceFunc(func() { held = false }), nil
					},
					direct: port, cluster: port,
				}
				result, err := test.invoke(d)
				if held || (ready && (err != nil || result == nil || result.Context != "lab")) ||
					(!ready && (result != nil || !errors.Is(err, refused))) {
					t.Fatalf("ready=%t held=%t result=%+v error=%v", ready, held, result, err)
				}
			}
		})
	}
}

func TestDelegatedSessionsRefuseBeforeCallingSessionPort(t *testing.T) {
	errorFromHold := errors.New("cannot hold admission")
	for _, delegated := range []delegatedSessions{
		{},
		{selection: func(context.Context) (string, error) { return "lab", nil }, hold: func(context.Context) (func(), error) {
			return nil, errorFromHold
		}},
	} {
		result, err := delegated.Exec(t.Context(), machineaccess.ExecRequest{})
		if result != nil || err == nil || (delegated.hold != nil && !errors.Is(err, errorFromHold)) {
			t.Fatalf("result=%+v error=%v", result, err)
		}
	}
}
