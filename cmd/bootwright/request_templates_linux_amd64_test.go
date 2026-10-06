//go:build linux && amd64

package main

import (
	"cmp"
	"context"
	"encoding/json"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// corpProxy is an external Proxy the controller may select, reached at url.
func corpProxy(url string) string {
	return "apiVersion: bootwright.io/v1alpha1\nkind: Proxy\nmetadata:\n  name: corp-proxy\n\nspec:\n  management: external\n\n  connection:\n    httpProxy: " + url + "\n"
}

// templatedRefusals compiles the named example with edits and added files and
// plans every bound capability as an apply of a context named after it does,
// then answers what each refused object's diagnostics say after " in ", two
// blocks of one object in their diagnostics' order. A graph admission refuses
// instead answers the objects that refusal names, with admitted false. Every
// planned request a runner could not encode must be refused. It also answers
// the planned blocks.
func templatedRefusals(t *testing.T, example string, edits []exampleEdit, added map[string]string) (map[string]string, []reconciliation.BlockDefinition, bool) {
	t.Helper()
	sources := exampleDirectory(t, example)
	for _, edit := range edits {
		index := slices.IndexFunc(sources.Files, func(file desiredstate.SourceFile) bool {
			return file.Path() == filepath.Join(sources.Roots[0], edit.file)
		})
		if index < 0 || strings.Count(string(sources.Files[index].Bytes()), edit.old) != 1 {
			t.Fatalf("%s does not hold %q exactly once", edit.file, edit.old)
		}
		body := strings.Replace(string(sources.Files[index].Bytes()), edit.old, edit.new, 1)
		sources.Files[index] = desiredstate.NewSourceFile(sources.Files[index].Path(), []byte(body))
	}
	for _, name := range slices.Sorted(maps.Keys(added)) {
		sources.Files = append(sources.Files, desiredstate.NewSourceFile(filepath.Join(sources.Roots[0], name), []byte(added[name])))
	}
	state, _, err := wireCompiler().Compile(context.Background(), sources)
	if err != nil {
		named := map[string]string{}
		for _, reported := range diagnostics.Of(err) {
			if reported.Object == nil {
				t.Fatalf("admission refused naming no object: %+v", reported)
			}
			named[reported.Object.Kind+"/"+reported.Object.Name] = reported.Message
		}
		return named, nil, false
	}
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t))
	input := lifecycle.PlanInput{
		Verb: reconciliation.Apply, State: state, Controller: "controller",
		Context: lifecycle.ContextIdentity{Name: example},
	}
	var definitions []reconciliation.BlockDefinition
	for _, binding := range resolver.Bindings() {
		capability, ok := resolver.Resolve(binding.Kind, binding.Implementation)
		if !ok {
			t.Fatalf("%s/%s does not resolve", binding.Kind, binding.Implementation)
		}
		contribution, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatalf("%s plan: %v", binding.Kind, diagnostics.Of(err))
		}
		definitions = append(definitions, contribution.Definitions...)
	}
	refused := map[string]string{}
	for _, reported := range diagnostics.Of(lifecycle.RefuseTemplateDelimiters(example, definitions)) {
		object := reported.Object.Kind + "/" + reported.Object.Name
		_, where, found := strings.Cut(reported.Message, `" in `)
		if reported.Code != "api.value" || !found || !strings.HasSuffix(reported.Remediation, "bootwright plan --context "+example) {
			t.Fatalf("%s refused as %+v", object, reported)
		}
		if earlier, repeated := refused[object]; repeated {
			where = earlier + "; " + where
		}
		refused[object] = where
	}
	for _, definition := range definitions {
		object := definition.Kind + "/" + definition.Object
		if _, err := ansible.ExtraVariables(map[string]any{"_request": json.RawMessage(definition.Request)}); err != nil && refused[object] == "" {
			t.Fatalf("the request planned for %s plans, but no runner can encode it: %v", object, err)
		}
	}
	return refused, definitions, true
}

// The audit proved each of these paths carries an authored string into a
// frozen request whose runner hands it to Ansible, every one passing
// validate. A fresh plan refuses each where the value reaches a request,
// naming every object planned from it and the field it reached, the
// controller's prerequisites included. An open NMState document carries its
// keys into a request as well: one ansible-core reserves refuses there, before
// registration, so no frozen request holds a key its runner cannot encode, and
// any other key plans. A path admission closes first is closed too, as long as
// admission names only the objects the value was authored on; that case is
// reported as skipped, so a lost planning proof stays visible.
func TestEveryProvenPathToARequestRefusesItsTemplateDelimiter(t *testing.T) {
	controllerEgress := exampleEdit{"infra/machines/controller.yaml", "  proxy:\n    direct: {}\n", "  proxy:\n    proxyRef: corp-proxy\n    noProxy:\n      - \"{{ 7*6 }}\"\n"}
	controllerProxy := exampleEdit{"infra/machines/controller.yaml", "  proxy:\n    direct: {}\n", "  proxy:\n    proxyRef: corp-proxy\n"}
	egress := func(where string) map[string]string {
		return map[string]string{
			"Environment/lab-rhel": where, "Proxy/lab-proxy": where, "DNSServer/lab-dns": where,
			"NTPServer/lab-ntp": where, "ArtifactServer/lab-artifacts": where,
		}
	}
	for _, test := range []struct {
		name    string
		example string
		edits   []exampleEdit
		added   map[string]string
		edited  []string
		want    map[string]string
		// carrier's request must hold carried, so a case that plans proves
		// its value reached a request.
		carrier, carried string
	}{
		{name: "the unedited example", want: map[string]string{}},
		{
			name:   "a package the install profile installs",
			edits:  []exampleEdit{{"infra/os/rhel-9-8.yaml", "        - chrony\n", "        - \"{{lookup('pipe','id')}}\"\n"}},
			edited: []string{"MachineInstallProfile/rhel-9-8"},
			want:   map[string]string{"Machine/rhel-01": "line * of kickstart"},
		},
		{
			name:  "the controller's noProxy",
			edits: []exampleEdit{controllerEgress},
			added: map[string]string{"infra/components/corp-proxy.yaml": corpProxy("http://proxy.example.test:8080")},
			// Admission holds the controller route to the proxy grammar on
			// the Environment that selects the controller Machine (D77).
			edited: []string{"Machine/controller", "Environment/lab-rhel"},
			want:   egress("egress.noProxy[0]"),
		},
		{
			name:   "the path of the controller's proxy URL",
			edits:  []exampleEdit{controllerProxy},
			added:  map[string]string{"infra/components/corp-proxy.yaml": corpProxy("http://proxy.example.test:8080/{{x}}")},
			edited: []string{"Proxy/corp-proxy"},
			want:   egress("egress.httpProxy"),
		},
		{
			name: "an artifact server listener's name",
			edits: []exampleEdit{
				{"infra/components/artifact-server.yaml", "    - name: http\n", "    - name: \"{{ 7*6 }}\"\n"},
				{"infra/components/artifact-server.yaml", "      listenerRef: http\n", "      listenerRef: \"{{ 7*6 }}\"\n"},
			},
			edited: []string{"ArtifactServer/lab-artifacts"},
			want:   map[string]string{"ArtifactServer/lab-artifacts": "endpoints[0].listener, and 1 more of its fields hold one"},
		},
		{
			name: "a name server endpoint's name",
			edits: []exampleEdit{
				{"infra/components/dns.yaml", "    - name: ip\n", "    - name: \"{# x #}\"\n"},
				{"infra/networkconfigs/lab-guests.yaml", "      endpointRef: ip\n", "      endpointRef: \"{# x #}\"\n"},
			},
			edited: []string{"DNSServer/lab-dns", "NetworkConfig/lab-guests"},
			want:   map[string]string{"DNSServer/lab-dns": "endpoints[0].name"},
		},
		{
			name:   "the libvirt bridge",
			edits:  []exampleEdit{{"infra/providers/lab-libvirt.yaml", "        bridge: virbr-lab\n", "        bridge: \"{%raw%}\"\n"}},
			edited: []string{"InfraProvider/lab-libvirt"},
			want:   map[string]string{"InfraProvider/lab-libvirt": "networks[0].bridge", "Machine/rhel-01": "interfaces[0].bridge"},
		},
		{
			name:    "a key ansible-core reserves in an open NMState document",
			example: "lab-sno",
			edits:   []exampleEdit{{"infra/networkconfigs/lab-guests.yaml", "        mtu: 1500\n", "        mtu: 1500\n        __ansible_vault: kept\n"}},
			edited:  []string{"NetworkConfig/lab-guests"},
			want:    map[string]string{"ContainerCluster/sno": "agentConfig.hosts[0].networkConfig.interfaces[0]"},
		},
		{
			name:    "an ordinary key of an open NMState document",
			example: "lab-sno",
			edits:   []exampleEdit{{"infra/networkconfigs/lab-guests.yaml", "        mtu: 1500\n", "        mtu: 1500\n        __ansible_note: kept\n"}},
			edited:  []string{"NetworkConfig/lab-guests"},
			want:    map[string]string{},
			carrier: "ContainerCluster/sno", carried: `"__ansible_note":"kept"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			refused, planned, admitted := templatedRefusals(t, cmp.Or(test.example, "lab-rhel"), test.edits, test.added)
			if !admitted {
				for object := range refused {
					if !slices.Contains(test.edited, object) {
						t.Fatalf("admission refused %s, which the case did not edit: %v", object, refused)
					}
				}
				t.Skipf("admission refuses the value before any plan, so planning is not exercised: %v", refused)
			}
			// The kickstart's line moves with its template, so a want of
			// "line * of kickstart" takes any line of it.
			if !maps.EqualFunc(refused, test.want, func(got, want string) bool {
				prefix, suffix, wildcard := strings.Cut(want, "*")
				return got == want || wildcard && strings.HasPrefix(got, prefix) && strings.HasSuffix(got, suffix) && !strings.Contains(got, ",")
			}) {
				t.Fatalf("planning refused %v, want %v", refused, test.want)
			}
			if test.carried != "" && !slices.ContainsFunc(planned, func(definition reconciliation.BlockDefinition) bool {
				return definition.Kind+"/"+definition.Object == test.carrier && strings.Contains(string(definition.Request), test.carried)
			}) {
				t.Fatalf("no request planned for %s holds %s", test.carrier, test.carried)
			}
		})
	}
}

// Planning and the runners' encoder each hold the keys ansible-core decodes as
// typed values, since planning may not import the embedded automation. A plan
// refuses exactly the keys the encoder cannot hand Ansible as data, so a
// request that plans always runs, observes and is removed, and no key that
// merely starts as theirs does strands a context.
func TestThePlanRefusesEveryKeyTheRunnersCannotEncode(t *testing.T) {
	for _, key := range []string{
		"__ansible_unsafe", "__ansible_vault", "__ansible_type",
		"__ansible", "__ansible_note", "__ansible_unsafe_", "_ansible_vault", "__ANSIBLE_TYPE", "ansible_type",
	} {
		request, err := json.Marshal(map[string]any{"interfaces": []any{map[string]any{key: "kept", "name": "enp1s0"}}})
		if err != nil {
			t.Fatal(err)
		}
		_, encoding := ansible.ExtraVariables(map[string]any{"_request": json.RawMessage(request)})
		planning := lifecycle.RefuseTemplateDelimiters("lab", []reconciliation.BlockDefinition{
			{ID: "alpha", Kind: "Machine", Object: "alpha", Request: request},
		})
		if (encoding != nil) != (planning != nil) {
			t.Errorf("the key %q: the runners' encoder refused %v, planning refused %v", key, encoding, diagnostics.Of(planning))
		}
	}
}
