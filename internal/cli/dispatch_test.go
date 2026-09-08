package cli

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/containercluster"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/nativeartifacts"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage"
	"github.com/crmarques/bootwright/internal/trust"
	"github.com/crmarques/bootwright/internal/workspace"
	"github.com/spf13/pflag"
)

func TestDispatchEveryApplicationCommand(t *testing.T) {
	ssh := machine.SSHOptions{IdentityFile: "key.pem", User: "operator", AskSudoPassword: true, UserForProvisioned: true}
	tests := []struct {
		path string
		want any
	}{
		{"context init", workspace.InitRequest{Name: "demo", InputDirectory: "inputs", SkipConfirmation: true}},
		{"context update", workspace.UpdateRequest{Name: "demo", InputDirectory: "inputs", SkipConfirmation: true}},
		{"context use", workspace.UseRequest{Name: "demo"}},
		{"context list", workspace.ListRequest{}},
		{"context current", workspace.CurrentRequest{Short: true}},
		{"context delete", workspace.DeleteRequest{Name: "demo", Purge: true, SkipConfirmation: true, AbandonResources: true}},
		{"add-ons list", addons.ListRequest{}},
		{"add-ons add", addons.AddRequest{Name: "demo", Version: "", SkipConfirmation: true}},
		{"add-ons delete", addons.DeleteRequest{Name: "demo", Version: "", SkipConfirmation: true}},
		{"secret set", secrets.SetRequest{ContextName: "example", Name: "demo", Source: secrets.Source{Kind: secrets.RawFileSource, File: "secret.bin"}, Username: "operator", SkipConfirmation: true}},
		{"secret generate", secrets.GenerateRequest{ContextName: "example", Renew: true}},
		{"secret check", secrets.CheckRequest{ContextName: "example"}},
		{"secret list", secrets.ListRequest{ContextName: "example"}},
		{"secret show", secrets.ShowRequest{ContextName: "example", Name: "demo", Part: "private"}},
		{"secret delete", secrets.DeleteRequest{ContextName: "example", Name: "demo", SkipConfirmation: true}},
		{"secret encryption init", secrets.EncryptionInitRequest{ContextName: "example"}},
		{"secret encryption status", secrets.EncryptionStatusRequest{ContextName: "example"}},
		{"secret encryption rotate", secrets.EncryptionRotateRequest{ContextName: "example", SkipConfirmation: true}},
		{"media add", managedos.AddMediaRequest{ContextName: "example", Name: "demo", SourceFile: "", SourceURL: "https://example.invalid/image.iso", SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", SkipConfirmation: true}},
		{"media list", managedos.ListMediaRequest{ContextName: "example", Checksums: true}},
		{"media delete", managedos.DeleteMediaRequest{ContextName: "example", Name: "demo", SkipConfirmation: true}},
		{"validate", desiredstate.ValidateRequest{ContextName: "", Files: []string{"inputs"}}},
		{"render effective", desiredstate.EffectiveRequest{ContextName: "example"}},
		{"preflight bastion", controller.CheckRequest{}},
		{"bastion setup", controller.SetupRequest{DryRun: true, SkipConfirmation: true}},
		{"preflight infra", environment.InfrastructurePreflightRequest{ContextName: "example", Clusters: []string{"first", "second"}, DryRun: true, TrustOnFirstUse: true, Verbose: true, SSH: ssh}},
		{"preflight clusters", environment.ClustersPreflightRequest{ContextName: "example", Clusters: []string{"first", "second"}, DryRun: true, TrustOnFirstUse: true, Verbose: true, SSH: ssh}},
		{"preflight all", environment.AllPreflightRequest{ContextName: "example", DryRun: true, TrustOnFirstUse: true, Verbose: true, SSH: ssh}},
		{"cluster list", environment.ListClustersRequest{ContextName: "example"}},
		{"cluster info", environment.ClusterInfoRequest{ContextName: "example", Name: "demo", Secrets: true}},
		{"cluster rsh", environment.ClusterRshRequest{ContextName: "example", Name: "demo", Node: "node-a", SSH: ssh}},
		{"cluster exec", environment.ClusterExecRequest{ContextName: "example", Name: "demo", Node: "node-a", SSH: ssh, Command: []string{"get", "--help", ""}}},
		{"preflight container-cluster", containercluster.PreflightRequest{ContextName: "example", Clusters: []string{"first", "second"}, DryRun: true, TrustOnFirstUse: true, Verbose: true, SSH: ssh}},
		{"preflight storage-cluster", storage.PreflightRequest{ContextName: "example", Clusters: []string{"first", "second"}, DryRun: true, TrustOnFirstUse: true, Verbose: true, SSH: ssh}},
		{"preflight add-ons", addons.PreflightRequest{ContextName: "example", Clusters: []string{"first", "second"}, SSH: ssh}},
		{"plan", reconciliation.PlanRequest{ContextName: "example"}},
		{"status", reconciliation.StatusRequest{ContextName: "example", Watch: true, WatchInterval: 7 * time.Second}},
		{"apply", reconciliation.ApplyRequest{ContextName: "example", Authorizations: []string{"data-loss"}, SkipConfirmation: true, Verbose: true, SSH: ssh}},
		{"destroy", reconciliation.DestroyRequest{ContextName: "example", Authorizations: []string{"data-loss"}, SkipConfirmation: true, Verbose: true, SSH: ssh}},
		{"render", nativeartifacts.RenderRequest{ContextName: "example", InputPath: "inputs", OutputDirectory: "artifacts", Clusters: []string{"first", "second"}, Sensitive: true}},
		{"render installer", containercluster.RenderInstallerRequest{ContextName: "example", Clusters: []string{"first", "second"}, Sensitive: true}},
		{"render storage", storage.RenderArtifactsRequest{ContextName: "example", Clusters: []string{"first", "second"}}},
		{"machine list", machine.ListRequest{ContextName: "example", Clusters: []string{"first", "second"}, Silent: true}},
		{"machine rsh", machine.RshRequest{ContextName: "example", Name: "demo", SSH: ssh}},
		{"machine exec", machine.ExecRequest{ContextName: "example", Name: "demo", SSH: ssh, Command: []string{"get", "--help", ""}}},
		{"machine trust", trust.EnrollRequest{ContextName: "example", Machines: []string{"node-a", "node-b"}, Replace: []string{"node-b"}, DryRun: true, SkipConfirmation: true}},
		{"cluster oc", containercluster.OCRequest{ContextName: "example", Name: "demo", Command: []string{"get", "--help", ""}}},
		{"cluster kubectl", containercluster.KubectlRequest{ContextName: "example", Name: "demo", Command: []string{"get", "--help", ""}}},
		{"cluster kubeconfig", containercluster.KubeconfigRequest{ContextName: "example", Name: "demo"}},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			failure := errors.New("application sentinel")
			record := &dispatchRecord{err: failure}
			ctx := context.WithValue(context.Background(), dispatchContextKey{}, tt.path)
			err := dispatchSpies(record).invoke(ctx, tt.path, dispatchFlags(), []string{"get", "--help", ""})
			if !errors.Is(err, failure) {
				t.Fatalf("dispatch error = %v, want application error", err)
			}
			if record.calls != 1 || record.path != tt.path || record.ctx != ctx {
				t.Fatalf("unexpected invocation: %#v", record)
			}
			if !reflect.DeepEqual(record.request, tt.want) {
				t.Fatalf("request = %#v, want %#v", record.request, tt.want)
			}
		})
	}
}

type dispatchContextKey struct{}

func TestDispatchFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		services Services
		path     string
		flags    *pflag.FlagSet
	}{
		{"missing service", Services{}, "plan", dispatchFlags()},
		{"missing flags", dispatchSpies(&dispatchRecord{}), "plan", nil},
		{"unknown command", dispatchSpies(&dispatchRecord{}), "removed", dispatchFlags()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.services.invoke(context.Background(), tc.path, tc.flags, nil); err == nil {
				t.Fatal("incomplete wiring succeeded")
			}
		})
	}
	record := &dispatchRecord{}
	err := dispatchSpies(record).invoke(context.Background(), "plan", pflag.NewFlagSet("incomplete", pflag.ContinueOnError), nil)
	if err == nil || record.calls != 0 {
		t.Fatalf("missing required flag: error=%v, calls=%d", err, record.calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = dispatchSpies(record).invoke(ctx, "plan", dispatchFlags(), nil)
	if !errors.Is(err, context.Canceled) || record.calls != 0 {
		t.Fatalf("canceled invocation: error=%v, calls=%d", err, record.calls)
	}
}

func TestDispatchOwnsMutableRequests(t *testing.T) {
	record := &dispatchRecord{}
	services := dispatchSpies(record)
	args := []string{"get", "pods"}
	flags := dispatchFlags()
	if err := services.invoke(context.Background(), "machine exec", flags, args); err != nil {
		t.Fatal(err)
	}
	request := record.request.(machine.ExecRequest)
	args[0] = "changed"
	if request.Command[0] != "get" {
		t.Fatal("request aliases caller arguments")
	}
	request.Command[1] = "mutated"
	if args[1] != "pods" {
		t.Fatal("caller aliases service request")
	}
	if err := services.invoke(context.Background(), "validate", flags, nil); err != nil {
		t.Fatal(err)
	}
	files := record.request.(desiredstate.ValidateRequest).Files
	files[0] = "mutated"
	original, err := flags.GetStringArray("file")
	if err != nil || original[0] != "inputs" {
		t.Fatalf("file request aliases flags: %v, %v", original, err)
	}
	if err := services.invoke(context.Background(), "apply", flags, nil); err != nil {
		t.Fatal(err)
	}
	record.request.(reconciliation.ApplyRequest).Authorizations[0] = "mutated"
	original, err = flags.GetStringArray("authorize")
	if err != nil || original[0] != "data-loss" {
		t.Fatalf("authorization request aliases flags: %v, %v", original, err)
	}
}

func TestDispatchSelectionAndSourceTranslation(t *testing.T) {
	t.Run("machine empty selection", func(t *testing.T) {
		for _, value := range []string{"", ",", "first,second"} {
			flags := dispatchFlags()
			if err := flags.Set("clusters", value); err != nil {
				t.Fatal(err)
			}
			record := &dispatchRecord{}
			if err := dispatchSpies(record).invoke(context.Background(), "machine list", flags, nil); err != nil {
				t.Fatal(err)
			}
			selection := record.request.(machine.ListRequest).Clusters
			if (selection == nil) != (value == "") {
				t.Fatalf("selection %q = %#v", value, selection)
			}
			if value == "," && len(selection) != 0 {
				t.Fatalf("explicit empty selection = %v", selection)
			}
		}
	})
	t.Run("add-on version", func(t *testing.T) {
		for _, tc := range []struct{ name, version, want string }{
			{"demo:1.2.3", "", "1.2.3"}, {"demo", "2.0.0", "2.0.0"}, {"demo", "", ""},
		} {
			flags := dispatchFlags()
			if err := flags.Set("name", tc.name); err != nil {
				t.Fatal(err)
			}
			if err := flags.Set("version", tc.version); err != nil {
				t.Fatal(err)
			}
			record := &dispatchRecord{}
			if err := dispatchSpies(record).invoke(context.Background(), "add-ons add", flags, nil); err != nil {
				t.Fatal(err)
			}
			request := record.request.(addons.AddRequest)
			if request.Name != "demo" || request.Version != tc.want {
				t.Fatalf("request = %#v", request)
			}
		}
	})
	t.Run("secret sources", func(t *testing.T) {
		for _, tc := range []struct {
			flags map[string]string
			want  secrets.Source
		}{
			{map[string]string{"pull-secret": "pull.json"}, secrets.Source{Kind: secrets.PullSecretSource, File: "pull.json"}},
			{map[string]string{"tls-cert": "cert.pem", "tls-key": "key.pem"}, secrets.Source{Kind: secrets.TLSSource, CertificateFile: "cert.pem", PrivateKeyFile: "key.pem"}},
			{map[string]string{"raw-file": "raw.bin"}, secrets.Source{Kind: secrets.RawFileSource, File: "raw.bin"}},
			{map[string]string{"from-file": "secret.yaml"}, secrets.Source{Kind: secrets.StructuredFileSource, File: "secret.yaml"}},
			{map[string]string{"password-stdin": "true"}, secrets.Source{Kind: secrets.PasswordStdinSource}},
			{map[string]string{"generate": "true"}, secrets.Source{Kind: secrets.GeneratedSource}},
		} {
			flags := dispatchFlags()
			if err := flags.Set("raw-file", ""); err != nil {
				t.Fatal(err)
			}
			for name, value := range tc.flags {
				if err := flags.Set(name, value); err != nil {
					t.Fatal(err)
				}
			}
			record := &dispatchRecord{}
			if err := dispatchSpies(record).invoke(context.Background(), "secret set", flags, nil); err != nil {
				t.Fatal(err)
			}
			if request := record.request.(secrets.SetRequest); request.Source != tc.want {
				t.Fatalf("source = %#v, want %#v", request.Source, tc.want)
			}
		}
	})
}
