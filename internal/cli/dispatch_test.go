package cli

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	addoncatalog "github.com/crmarques/bootwright/internal/addons/catalog"
	addonpreflight "github.com/crmarques/bootwright/internal/addons/preflight"
	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/containercluster/installation"
	containerpreflight "github.com/crmarques/bootwright/internal/containercluster/preflight"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	environmentaccess "github.com/crmarques/bootwright/internal/environment/access"
	"github.com/crmarques/bootwright/internal/environment/inspection"
	environmentpreflight "github.com/crmarques/bootwright/internal/environment/preflight"
	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/managedos/media"
	artifactrendering "github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/encryption"
	storagepreflight "github.com/crmarques/bootwright/internal/storage/preflight"
	storagerendering "github.com/crmarques/bootwright/internal/storage/rendering"
	"github.com/crmarques/bootwright/internal/trust/enrollment"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
	"github.com/spf13/pflag"
)

func TestDispatchEveryApplicationCommand(t *testing.T) {
	ssh := machine.SSHOptions{IdentityFile: "key.pem", User: "operator", AskSudoPassword: true, UserForProvisioned: true}
	tests := []struct {
		path string
		want any
	}{
		{"context init", contexts.InitRequest{Name: "demo", ConfigurationFile: "inputs", InputDirectory: "inputs"}},
		{"context update", contexts.UpdateRequest{Name: "demo", ConfigurationFile: "inputs", InputDirectory: "inputs", SkipConfirmation: true}},
		{"context use", contexts.UseRequest{Name: "demo"}},
		{"context list", contexts.ListRequest{}},
		{"context current", contexts.CurrentRequest{Short: true}},
		{"context delete", contexts.DeleteRequest{Name: "demo", Purge: true, SkipConfirmation: true}},
		{"add-ons list", addoncatalog.ListRequest{}},
		{"add-ons add", addoncatalog.AddRequest{Name: "demo", Version: "", SkipConfirmation: true}},
		{"add-ons delete", addoncatalog.DeleteRequest{Name: "demo", Version: "", SkipConfirmation: true}},
		{"secret set", custody.SetRequest{ContextName: "example", Name: "demo", Input: secrets.Input{ValueFile: "secret.bin"}, SkipConfirmation: true}},
		{"secret generate", custody.GenerateRequest{ContextName: "example", Name: "demo", Renew: true}},
		{"secret check", custody.CheckRequest{ContextName: "example"}},
		{"secret list", custody.ListRequest{ContextName: "example"}},
		{"secret show", custody.ShowRequest{ContextName: "example", Name: "demo", Part: secrets.PrivateKeyPart}},
		{"secret delete", custody.DeleteRequest{ContextName: "example", Name: "demo", SkipConfirmation: true}},
		{"secret encryption init", encryption.EncryptionInitRequest{ContextName: "example"}},
		{"secret encryption status", encryption.EncryptionStatusRequest{ContextName: "example"}},
		{"secret encryption rotate", encryption.EncryptionRotateRequest{ContextName: "example", SkipConfirmation: true}},
		{"media add", media.AddMediaRequest{Name: "demo", SourceFile: "", SourceURL: "https://example.invalid/image.iso", SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", SkipConfirmation: true}},
		{"media list", media.ListMediaRequest{Checksums: true}},
		{"media delete", media.DeleteMediaRequest{Name: "demo", SkipConfirmation: true}},
		{"validate", compilation.ValidateRequest{ContextName: "", Files: []string{"inputs"}}},
		{"render effective", compilation.EffectiveRequest{ContextName: "example"}},
		{"preflight controller", prerequisites.CheckRequest{ContextName: "example"}},
		{"setup", prerequisites.SetupRequest{DryRun: true, SkipConfirmation: true, PurgeOldBundles: true}},
		{"preflight infra", environmentpreflight.InfrastructurePreflightRequest{ContextName: "example", Clusters: []string{"first", "second"}, DryRun: true, TrustOnFirstUse: true, Verbose: true, SSH: ssh}},
		{"preflight clusters", environmentpreflight.ClustersPreflightRequest{ContextName: "example", Clusters: []string{"first", "second"}, DryRun: true, TrustOnFirstUse: true, Verbose: true, SSH: ssh}},
		{"preflight all", environmentpreflight.AllPreflightRequest{ContextName: "example", DryRun: true, TrustOnFirstUse: true, Verbose: true, SSH: ssh}},
		{"cluster list", inspection.ListClustersRequest{ContextName: "example"}},
		{"cluster info", inspection.ClusterInfoRequest{ContextName: "example", Name: "demo", Secrets: true}},
		{"cluster rsh", environmentaccess.ClusterRshRequest{ContextName: "example", Name: "demo", Node: "node-a", SSH: ssh}},
		{"cluster exec", environmentaccess.ClusterExecRequest{ContextName: "example", Name: "demo", Node: "node-a", SSH: ssh, Command: []string{"get", "--help", ""}}},
		{"preflight container-cluster", containerpreflight.PreflightRequest{ContextName: "example", Clusters: []string{"first", "second"}, DryRun: true, TrustOnFirstUse: true, Verbose: true, SSH: ssh}},
		{"preflight storage-cluster", storagepreflight.PreflightRequest{ContextName: "example", Clusters: []string{"first", "second"}, DryRun: true, TrustOnFirstUse: true, Verbose: true, SSH: ssh}},
		{"preflight add-ons", addonpreflight.PreflightRequest{ContextName: "example", Clusters: []string{"first", "second"}, SSH: ssh}},
		{"plan", lifecycle.PlanRequest{ContextName: "example", Stages: []string{"infra-components", "substrates"}}},
		{"status", lifecycle.StatusRequest{ContextName: "example", Watch: true, WatchInterval: 7 * time.Second}},
		{"apply", lifecycle.ApplyRequest{ContextName: "example", Stages: []string{"infra-components", "substrates"}, Authorizations: []string{"data-loss"}, SkipConfirmation: true, Verbose: true, SSH: ssh}},
		{"destroy", lifecycle.DestroyRequest{ContextName: "example", Authorizations: []string{"data-loss"}, SkipConfirmation: true, Verbose: true, SSH: ssh}},
		{"render", artifactrendering.RenderRequest{ContextName: "example", InputPath: "inputs", OutputDirectory: "artifacts", Clusters: []string{"first", "second"}, Sensitive: true}},
		{"render installer", installation.RenderInstallerRequest{ContextName: "example", Clusters: []string{"first", "second"}, Sensitive: true}},
		{"render storage", storagerendering.RenderArtifactsRequest{ContextName: "example", Clusters: []string{"first", "second"}}},
		{"machine list", inventory.ListRequest{ContextName: "example", Clusters: []string{"first", "second"}, Silent: true}},
		{"machine rsh", machineaccess.RshRequest{ContextName: "example", Name: "demo", SSH: ssh}},
		{"machine exec", machineaccess.ExecRequest{ContextName: "example", Name: "demo", SSH: ssh, Command: []string{"get", "--help", ""}}},
		{"machine trust", enrollment.EnrollRequest{ContextName: "example", Machines: []string{"node-a", "node-b"}, Replace: []string{"node-b"}, DryRun: true, SkipConfirmation: true}},
		{"cluster oc", containeraccess.OCRequest{ContextName: "example", Name: "demo", Command: []string{"get", "--help", ""}}},
		{"cluster kubectl", containeraccess.KubectlRequest{ContextName: "example", Name: "demo", Command: []string{"get", "--help", ""}}},
		{"cluster kubeconfig", containeraccess.KubeconfigRequest{ContextName: "example", Name: "demo"}},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			failure := errors.New("application sentinel")
			record := &dispatchRecord{err: failure}
			ctx := context.WithValue(context.Background(), dispatchContextKey{}, tt.path)
			_, err := dispatchSpies(record).invoke(ctx, tt.path, dispatchFlags(), []string{"get", "--help", ""})
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

func TestDispatchPreservesTypedValidationResult(t *testing.T) {
	want := &compilation.Report{Counts: compilation.Counts{FilesSeen: 2, ObjectsDecoded: 3}}
	record := &dispatchRecord{report: want}
	got, err := dispatchSpies(record).invoke(context.Background(), "validate", dispatchFlags(), nil)
	if err != nil || got.validation != want || record.calls != 1 {
		t.Fatalf("typed result = %#v, %v; calls = %d", got, err, record.calls)
	}
}

func TestEnvironmentCapabilitiesAreIndependent(t *testing.T) {
	for _, capability := range []struct {
		name     string
		commands []string
		wire     func(*dispatchRecord) Services
	}{
		{"preflight", []string{"preflight infra", "preflight clusters", "preflight all"}, func(record *dispatchRecord) Services {
			return Services{EnvironmentPreflight: environmentSpy{record}}
		}},
		{"inspection", []string{"cluster list", "cluster info"}, func(record *dispatchRecord) Services {
			return Services{EnvironmentInspection: environmentSpy{record}}
		}},
		{"access", []string{"cluster rsh", "cluster exec"}, func(record *dispatchRecord) Services {
			return Services{EnvironmentAccess: environmentSpy{record}}
		}},
	} {
		t.Run(capability.name, func(t *testing.T) {
			for _, command := range capability.commands {
				record := &dispatchRecord{}
				if _, err := capability.wire(record).invoke(context.Background(), command, dispatchFlags(), nil); err != nil {
					t.Fatalf("%s requires an unrelated service: %v", command, err)
				}
				if record.calls != 1 || record.path != command {
					t.Fatalf("%s invoked the wrong capability: %#v", command, record)
				}
				if _, err := (Services{}).invoke(context.Background(), command, dispatchFlags(), nil); !errors.Is(err, errMissingService) {
					t.Fatalf("%s without its service returned %v", command, err)
				}
			}
		})
	}
}

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
			if _, err := tc.services.invoke(context.Background(), tc.path, tc.flags, nil); err == nil {
				t.Fatal("incomplete wiring succeeded")
			}
		})
	}
	record := &dispatchRecord{}
	_, err := dispatchSpies(record).invoke(context.Background(), "plan", pflag.NewFlagSet("incomplete", pflag.ContinueOnError), nil)
	if err == nil || record.calls != 0 {
		t.Fatalf("missing required flag: error=%v, calls=%d", err, record.calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = dispatchSpies(record).invoke(ctx, "plan", dispatchFlags(), nil)
	if !errors.Is(err, context.Canceled) || record.calls != 0 {
		t.Fatalf("canceled invocation: error=%v, calls=%d", err, record.calls)
	}
}

func TestDispatchOwnsMutableRequests(t *testing.T) {
	record := &dispatchRecord{}
	services := dispatchSpies(record)
	args := []string{"get", "pods"}
	flags := dispatchFlags()
	if _, err := services.invoke(context.Background(), "machine exec", flags, args); err != nil {
		t.Fatal(err)
	}
	request := record.request.(machineaccess.ExecRequest)
	args[0] = "changed"
	if request.Command[0] != "get" {
		t.Fatal("request aliases caller arguments")
	}
	request.Command[1] = "mutated"
	if args[1] != "pods" {
		t.Fatal("caller aliases service request")
	}
	if _, err := services.invoke(context.Background(), "validate", flags, nil); err != nil {
		t.Fatal(err)
	}
	files := record.request.(compilation.ValidateRequest).Files
	files[0] = "mutated"
	original, err := flags.GetStringArray("file")
	if err != nil || original[0] != "inputs" {
		t.Fatalf("file request aliases flags: %v, %v", original, err)
	}
	if _, err := services.invoke(context.Background(), "apply", flags, nil); err != nil {
		t.Fatal(err)
	}
	record.request.(lifecycle.ApplyRequest).Authorizations[0] = "mutated"
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
			if _, err := dispatchSpies(record).invoke(context.Background(), "machine list", flags, nil); err != nil {
				t.Fatal(err)
			}
			selection := record.request.(inventory.ListRequest).Clusters
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
			if _, err := dispatchSpies(record).invoke(context.Background(), "add-ons add", flags, nil); err != nil {
				t.Fatal(err)
			}
			request := record.request.(addoncatalog.AddRequest)
			if request.Name != "demo" || request.Version != tc.want {
				t.Fatalf("request = %#v", request)
			}
		}
	})
	t.Run("secret inputs", func(t *testing.T) {
		for _, tc := range []struct {
			flags map[string]string
			want  secrets.Input
		}{
			{map[string]string{"value-file": "value.bin"}, secrets.Input{Provided: secrets.ValueFileInput, ValueFile: "value.bin"}},
			{map[string]string{"value-stdin": "true"}, secrets.Input{Provided: secrets.ValueStdinInput, ValueStdin: true}},
			{map[string]string{"username": "operator", "password-file": "password"}, secrets.Input{Provided: secrets.UsernameInput | secrets.PasswordFileInput, Username: "operator", PasswordFile: "password"}},
			{map[string]string{"username": "operator", "password-stdin": "true"}, secrets.Input{Provided: secrets.UsernameInput | secrets.PasswordStdinInput, Username: "operator", PasswordStdin: true}},
			{map[string]string{"certificate-file": "cert.pem"}, secrets.Input{Provided: secrets.CertificateFileInput, CertificateFile: "cert.pem"}},
			{map[string]string{"certificate-file": "cert.pem", "private-key-file": "key.pem"}, secrets.Input{Provided: secrets.CertificateFileInput | secrets.PrivateKeyFileInput, CertificateFile: "cert.pem", PrivateKeyFile: "key.pem"}},
			{map[string]string{"private-key-file": "key.pem"}, secrets.Input{Provided: secrets.PrivateKeyFileInput, PrivateKeyFile: "key.pem"}},
			{map[string]string{"private-key-file": "key.pem", "public-key-file": "key.pub"}, secrets.Input{Provided: secrets.PrivateKeyFileInput | secrets.PublicKeyFileInput, PrivateKeyFile: "key.pem", PublicKeyFile: "key.pub"}},
			{map[string]string{"value-file": "value.bin", "password-stdin": "false"}, secrets.Input{Provided: secrets.ValueFileInput | secrets.PasswordStdinInput, ValueFile: "value.bin"}},
			{map[string]string{"value-file": "value.bin", "username": ""}, secrets.Input{Provided: secrets.ValueFileInput | secrets.UsernameInput, ValueFile: "value.bin"}},
		} {
			flags := dispatchFlags()
			if err := flags.Lookup("value-file").Value.Set(""); err != nil {
				t.Fatal(err)
			}
			for name, value := range tc.flags {
				if err := flags.Set(name, value); err != nil {
					t.Fatal(err)
				}
			}
			record := &dispatchRecord{}
			if _, err := dispatchSpies(record).invoke(context.Background(), "secret set", flags, nil); err != nil {
				t.Fatal(err)
			}
			if request := record.request.(custody.SetRequest); request.Input != tc.want {
				t.Fatalf("input = %#v, want %#v", request.Input, tc.want)
			}
		}
	})
}
