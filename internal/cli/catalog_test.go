package cli

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestPublicCatalog(t *testing.T) {
	expected := map[string]string{
		"context init": "file input-dir name", "context update": "file input-dir name yes", "context use": "name", "context list": "", "context current": "short", "context delete": "name purge yes",
		"add-ons list": "output", "add-ons add": "name version yes", "add-ons delete": "name yes",
		"secret set": "certificate-file name password-file password-stdin private-key-file public-key-file username value-file value-stdin yes", "secret generate": "name renew", "secret check": "output", "secret list": "output", "secret show": "name part", "secret delete": "name yes",
		"secret encryption init": "", "secret encryption status": "output", "secret encryption rotate": "yes",
		"media add": "from-file from-url name sha256 yes", "media list": "checksums output", "media delete": "name yes",
		"validate": "file output", "preflight controller": "", "preflight infra": "clusters dry-run output trust-on-first-use verbose", "preflight clusters": "clusters dry-run output trust-on-first-use verbose", "preflight container-cluster": "clusters dry-run output trust-on-first-use verbose", "preflight storage-cluster": "clusters dry-run output trust-on-first-use verbose", "preflight add-ons": "clusters output", "preflight all": "dry-run output trust-on-first-use verbose",
		"plan": "stage", "status": "output watch watch-interval", "render": "clusters input-dir output output-dir sensitive", "render effective": "output", "render installer": "clusters output sensitive", "render storage": "clusters output", "apply": "authorize stage verbose yes", "destroy": "authorize verbose yes",
		"machine list": "clusters output silent", "machine rsh": "name", "machine exec": "name", "machine start": "name output", "machine stop": "force name output yes", "machine restart": "force name output yes", "machine trust": "dry-run machines output replace yes", "setup": "dry-run purge-old-bundles yes",
		"cluster list": "output", "cluster info": "name output secrets", "cluster rsh": "name node", "cluster exec": "name node", "cluster oc": "name", "cluster kubectl": "name", "cluster kubeconfig": "name",
		"version": "", "help": "", "completion bash": "no-descriptions", "completion zsh": "no-descriptions", "completion fish": "no-descriptions", "completion powershell": "no-descriptions",
	}
	root, err := newCommandTree(New(Config{}))
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		if command.Hidden {
			return
		}
		if command.Runnable() {
			path := strings.TrimPrefix(command.CommandPath(), "bootwright ")
			var flags []string
			command.LocalNonPersistentFlags().VisitAll(func(flag *pflag.Flag) {
				flags = append(flags, flag.Name)
				wantShort := ""
				if flag.Name == "file" {
					wantShort = "f"
				}
				if flag.Name == "verbose" {
					wantShort = "v"
				}
				if flag.Shorthand != wantShort {
					t.Errorf("%s --%s shorthand %q", path, flag.Name, flag.Shorthand)
				}
				if flag.Name == "output" && !reflect.DeepEqual(flag.Annotations["bootwright.enum"], []string{"text", "json"}) {
					t.Errorf("%s output enum", path)
				}
				if path == "secret encryption init" && flag.Name == "type" && !reflect.DeepEqual(flag.Annotations["bootwright.catalog"], []string{secretEncryptionTypeCatalog}) {
					t.Errorf("%s type catalog", path)
				}
			})
			slices.Sort(flags)
			found[path] = strings.Join(flags, " ")
		}
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(root)
	if !reflect.DeepEqual(found, expected) {
		t.Errorf("public catalog mismatch\ngot %#v\nwant %#v", found, expected)
	}
	var globals []string
	root.PersistentFlags().VisitAll(func(flag *pflag.Flag) { globals = append(globals, flag.Name) })
	if strings.Join(globals, " ") != "context help ssh-ask-sudo-password ssh-id-file ssh-user ssh-user-for-provisioned" {
		t.Errorf("global flags: %v", globals)
	}
	if root.PersistentFlags().ShorthandLookup("h").Name != "help" {
		t.Fatal("missing help shorthand")
	}
}
