//go:build linux && amd64

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
)

type credentialVersionCustody interface {
	PrepareCredentialVersion(context.Context, custody.RebindRequest) (custody.ScopedCredentialVersion, error)
	Bind(context.Context, custody.BindRequest) (secretstore.Binding, error)
	Reopen(context.Context, custody.BindingRequest) ([]secretstore.BoundMaterial, error)
	ReadCurrent(context.Context, custody.ReadCurrentRequest) ([]secretstore.BoundMaterial, error)
}

func reopenedVersion(t *testing.T, reading func() ([]secretstore.BoundMaterial, error)) (string, string) {
	t.Helper()
	read, err := reading()
	if err != nil || len(read) != 1 {
		t.Fatalf("read %d bound items (%v)", len(read), err)
	}
	defer read[0].Material.Clear()
	value, _ := read[0].Material.Part(secrets.ValuePart)
	defer clear(value)
	return read[0].Version.ID, string(value)
}

func credentialVersionContext(t *testing.T, declaration string, seed func(cli.Services)) (cli.Services, *contextfs.Store) {
	t.Helper()
	parent := t.TempDir()
	input, root := filepath.Join(parent, "input"), filepath.Join(parent, "state")
	if err := os.Mkdir(input, 0700); err != nil {
		t.Fatal(err)
	}
	addSecretInput(t, input, "environment.yaml", syntheticEnvironment)
	addSecretInput(t, input, "controller.yaml", serviceHost)
	addSecretInput(t, input, "secret.yaml", declaration)
	repository := testRepository(root)
	deps := testContextWiring(t, root)
	deps.Repository, deps.Workspace, deps.Trust = repository, repository, repository
	deps.Lifecycle = lifecycleDependencies{
		Workspace: repository,
		Host:      testLifecycleHost{}, Guard: testLifecycleGuard{}, Selection: deps.Selection,
		Presenter: testLifecyclePresenter{}, Confirmer: deps.Confirmer, Capabilities: removableCapabilities{},
	}
	services := assembleServices(deps)
	contextRun(t, services, 0, "init", "--name", "alpha", "--input-dir", input)
	contextRun(t, services, 0, "secret", "encryption", "init")
	seed(services)
	return services, repository
}

func TestACredentialVersionOverTheRealKeyringIsCurrentPinnedAndHeldInEveryWindow(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, declaration, candidate string
		seed                         func(*testing.T, cli.Services)
		replace                      func(*testing.T, cli.Services) string
		bulk                         []string
	}{
		{
			name: "secret set", declaration: secretDocument("grafana", "opaque", ""), candidate: "synthetic-candidate-password",
			seed: func(t *testing.T, services cli.Services) {
				contextRun(t, services, 0, "secret", "set", "--name", "grafana", "--value-file", addSecretInput(t, t.TempDir(), "value", "synthetic-bound-password"))
			},
			replace: func(t *testing.T, services cli.Services) string {
				_, stderr := contextRun(t, services, 1, "secret", "set", "--name", "grafana", "--yes", "--value-file", addSecretInput(t, t.TempDir(), "value", "synthetic-replacement"))
				return stderr
			},
		},
		{
			name: "secret generate --renew", declaration: secretDocument("grafana", "token", "  source: {generated: {}}\n"), candidate: "c3ludGhldGljLWNhbmRpZGF0ZS10b2tlbi12YWx1ZS0",
			seed: func(t *testing.T, services cli.Services) {
				contextRun(t, services, 0, "secret", "generate", "--name", "grafana", "--yes")
			},
			replace: func(t *testing.T, services cli.Services) string {
				_, stderr := contextRun(t, services, 1, "secret", "generate", "--name", "grafana", "--renew", "--yes")
				return stderr
			},
			bulk: []string{"secret", "generate", "--renew", "--yes"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			services, repository := credentialVersionContext(t, test.declaration, func(services cli.Services) { test.seed(t, services) })
			composed, ok := services.Secrets.(credentialVersionCustody)
			if !ok {
				t.Fatal("the composed custody service offers no credential version")
			}
			bound, err := composed.Bind(ctx, custody.BindRequest{ContextName: "alpha", Names: []string{"grafana"}})
			if err != nil || len(bound.Versions) != 1 {
				t.Fatalf("bound %+v (%v)", bound, err)
			}
			applied := failedApplyNaming(t, repository, bound.ID)
			candidate := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(test.candidate)})
			defer candidate.Clear()
			current := func() ([]secretstore.BoundMaterial, error) {
				return composed.ReadCurrent(ctx, custody.ReadCurrentRequest{ContextName: "alpha", Names: []string{"grafana"}})
			}
			publish := func(steps func(custody.ScopedCredentialVersion) []custody.CredentialPublication, named bool) secretstore.Binding {
				t.Helper()
				write, err := composed.PrepareCredentialVersion(ctx, custody.RebindRequest{ContextName: "alpha", BindingID: bound.ID, Names: []string{"grafana"}})
				if err != nil {
					t.Fatal(err)
				}
				var issued secretstore.Binding
				err = repository.MutateLifecycle(ctx, "alpha", func(tx lifecycle.Transaction) error {
					for _, step := range steps(write) {
						if err := tx.Secrets(ctx, func(selected secretstore.Context, area secretstore.Area) error {
							var err error
							issued, err = step(ctx, selected, area, bound.Versions[0], candidate)
							return err
						}); err != nil {
							return err
						}
					}
					if !named {
						return nil
					}
					store := operationstore.New(tx.Operations(), tx.Logs(), exitStamp)
					record, err := store.ReadOperation(ctx, applied)
					if err != nil {
						return err
					}
					record.Bindings = []string{issued.ID}
					return store.UpdateOperation(ctx, record)
				})
				if err != nil {
					t.Fatalf("%v %+v", err, diagnostics.Of(err))
				}
				return issued
			}
			refusesAsHeld := func(window string) {
				t.Helper()
				held, _ := reopenedVersion(t, current)
				if stderr := test.replace(t, services); !strings.Contains(stderr, "secret.store.conflict") || !strings.Contains(stderr, "is held by the apply "+applied) {
					t.Fatalf("%s over the version %s refused with\n%s", window, held, stderr)
				}
				if test.bulk != nil {
					contextRun(t, services, 0, test.bulk...)
				}
				if after, _ := reopenedVersion(t, current); after != held {
					t.Fatalf("a replacement %s moved the current version to %s", window, after)
				}
			}
			publish(func(write custody.ScopedCredentialVersion) []custody.CredentialPublication {
				return []custody.CredentialPublication{write.Write}
			}, false)
			written, value := reopenedVersion(t, current)
			if written == bound.Versions[0] || value != test.candidate {
				t.Fatalf("the current version %s holds %q, not a new version of the candidate", written, value)
			}
			refusesAsHeld("between the version and the rebind")
			unnamed := publish(func(write custody.ScopedCredentialVersion) []custody.CredentialPublication {
				return []custody.CredentialPublication{write.Rebind}
			}, false)
			refusesAsHeld("between the rebind and the operation record")
			issued := publish(func(write custody.ScopedCredentialVersion) []custody.CredentialPublication {
				return []custody.CredentialPublication{write.Write, write.Rebind}
			}, true)
			if held, _ := reopenedVersion(t, current); held != written {
				t.Fatalf("the repeat wrote another version %s", held)
			}
			pinned, value := reopenedVersion(t, func() ([]secretstore.BoundMaterial, error) {
				return composed.Reopen(ctx, custody.BindingRequest{ContextName: "alpha", BindingID: issued.ID})
			})
			if issued.ID == bound.ID || issued.ID == unnamed.ID || pinned != written || value != test.candidate {
				t.Fatalf("binding %s pins %s, not the current %s", issued.ID, pinned, written)
			}
			refusesAsHeld("once the operation record names the new binding")
		})
	}
}
