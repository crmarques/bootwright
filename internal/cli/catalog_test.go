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
		"context init": "file input-dir name", "context update": "file input-dir name yes", "context use": "name", "context list": "", "context current": "short", "context delete": "allow-orphans name purge yes",
		"add-ons list": "",
		"secret set":   "certificate-file name password-file password-stdin private-key-file public-key-file username value-file value-stdin yes", "secret generate": "name renew", "secret check": "", "secret list": "", "secret info": "name", "secret show": "name part", "secret delete": "name yes",
		"secret encryption init": "", "secret encryption status": "", "secret encryption rotate": "yes",
		"media add": "from-file from-url from-url-file name sha256 yes", "media list": "checksums", "media delete": "name yes",
		"validate": "file", "preflight controller": "", "preflight infra": "dry-run", "preflight clusters": "clusters dry-run trust-on-first-use", "preflight container-cluster": "clusters dry-run", "preflight storage-cluster": "clusters dry-run trust-on-first-use", "preflight add-ons": "clusters", "preflight all": "dry-run trust-on-first-use",
		"plan": "stage until", "status": "", "diff": "exit-code observe", "render": "input-dir output-dir sensitive", "render effective": "origins", "render installer": "clusters sensitive", "render storage": "clusters", "apply": "authorize stage until yes", "destroy": "authorize unregistered yes",
		"machine list": "clusters power-status short", "machine info": "name", "machine rsh": "name", "machine exec": "name", "machine start": "name", "machine stop": "force name yes", "machine restart": "force name yes", "machine trust": "dry-run expect name replace yes", "controller install": "dest-dir download-only dry-run wheel-mirror yes", "controller rebind": "machine-id product-uuid recorded-host-retired root-uuid yes", "controller uninstall": "keep-packages yes",
		"cluster list": "", "cluster info": "name object-user secrets", "cluster rsh": "name node", "cluster exec": "name node", "cluster oc": "name", "cluster kubectl": "name", "cluster kubeconfig": "name", "cluster keyring": "client name",
		"version": "", "schema": "format", "help": "", "completion bash": "no-descriptions", "completion zsh": "no-descriptions", "completion fish": "no-descriptions", "completion powershell": "no-descriptions",
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
				if flag.Shorthand != wantShort {
					t.Errorf("%s --%s shorthand %q", path, flag.Name, flag.Shorthand)
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
	if strings.Join(globals, " ") != "ascii color context detail help output progress ssh-ask-sudo-password ssh-id-file ssh-user ssh-user-for-provisioned width" {
		t.Errorf("global flags: %v", globals)
	}
	for short, name := range map[string]string{"h": "help", "o": "output"} {
		flag := root.PersistentFlags().ShorthandLookup(short)
		if flag == nil || flag.Name != name {
			t.Errorf("missing -%s shorthand for --%s", short, name)
		}
	}
	for _, expected := range []struct {
		name, initial string
		values        []string
	}{
		{"output", "text", []string{"text", "json", "yaml"}},
		{"color", "auto", []string{"auto", "always", "never"}},
		{"progress", "auto", []string{"auto", "always", "never"}},
		{"detail", "compact", []string{"compact", "full"}},
		{"width", "0", nil}, {"ascii", "false", nil},
	} {
		flag := root.PersistentFlags().Lookup(expected.name)
		if flag == nil || flag.DefValue != expected.initial || !slices.Equal(flag.Annotations["bootwright.enum"], expected.values) {
			t.Errorf("global --%s: %#v", expected.name, flag)
		}
	}
}

func TestCatalogFlagSpellingsFollowTheSelectorRules(t *testing.T) {
	checked := 0
	for _, spec := range commandCatalog() {
		own := strings.Fields(spec.path)[0]
		for _, flag := range spec.flags {
			checked++
			if flag.name == own+"s" {
				t.Errorf("%s has --%s, the plural of its own noun; --name selects its own instances", spec.path, flag.name)
			}
			names := strings.Contains(strings.ToLower(flag.help), "only") && strings.Contains(strings.ToLower(flag.help), "name")
			if names && flag.name != "short" {
				t.Errorf("%s has --%s printing names only; --short is the one flag that does", spec.path, flag.name)
			}
		}
	}
	if checked < 100 {
		t.Fatalf("read %d flags; the catalog has changed shape", checked)
	}
	for path, flags := range map[string][]string{"machine trust": {"name"}, "machine list": {"clusters", "short"}, "context current": {"short"}} {
		for _, want := range flags {
			if !slices.ContainsFunc(specFor(path).flags, func(flag flagSpec) bool { return flag.name == want }) {
				t.Errorf("%s lacks --%s", path, want)
			}
		}
	}
}
