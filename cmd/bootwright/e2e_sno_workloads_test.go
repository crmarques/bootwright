package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"
)

type snoWorkloadObject struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name        string            `yaml:"name"`
		Namespace   string            `yaml:"namespace"`
		Labels      map[string]string `yaml:"labels"`
		Annotations map[string]string `yaml:"annotations"`
	} `yaml:"metadata"`
	Spec struct {
		Domain            string `yaml:"domain"`
		Host              string `yaml:"host"`
		NamespaceSelector struct {
			MatchLabels map[string]string `yaml:"matchLabels"`
		} `yaml:"namespaceSelector"`
		Egress []struct {
			Type string `yaml:"type"`
			To   struct {
				CIDRSelector string `yaml:"cidrSelector"`
			} `yaml:"to"`
		} `yaml:"egress"`
		Template struct {
			Metadata struct {
				Annotations map[string]string `yaml:"annotations"`
			} `yaml:"metadata"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

type snoWorkloadEgressPolicy struct {
	Spec struct {
		Egress []struct {
			To []struct {
				IPBlock struct {
					Except []string `yaml:"except"`
				} `yaml:"ipBlock"`
			} `yaml:"to"`
		} `yaml:"egress"`
	} `yaml:"spec"`
}

func readSNOWorkload[T any](t *testing.T, path string) T {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var object T
	if err := yaml.Unmarshal(data, &object); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return object
}

// The static workloads/ tree is applied by the parent runbook and the
// isolation chart renders its own objects from values.yaml; the facts both
// carry (names, domains, networks, ranges) have values.yaml as their one
// owner, so a default that moves there fails here until workloads/ follows.
func TestSNOStaticWorkloadsFollowTheCaseDefaults(t *testing.T) {
	root := filepath.Join("..", "..", "e2e", "lab-sno-fips-controller")
	var values struct {
		Host struct {
			Domain string `yaml:"domain"`
			CIDR   string `yaml:"cidr"`
		} `yaml:"host"`
		Environments map[string]any `yaml:"environments"`
	}
	data, err := os.ReadFile(filepath.Join(root, "values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	cidrs := map[string]string{}
	for _, name := range []string{"staging", "production"} {
		environment, _ := values.Environments[name].(map[string]any)
		cidr, _ := environment["cidr"].(string)
		if cidr == "" {
			t.Fatalf("values.yaml environments.%s.cidr is empty", name)
		}
		cidrs[name] = cidr
	}
	other := map[string]string{"staging": "production", "production": "staging"}
	for _, name := range []string{"staging", "production"} {
		t.Run(name, func(t *testing.T) {
			directory := filepath.Join(root, "workloads", name)
			namespace := readSNOWorkload[snoWorkloadObject](t, filepath.Join(directory, "namespace.yaml"))
			if namespace.Metadata.Name != name || namespace.Metadata.Labels["lab.example.test/environment"] != name {
				t.Errorf("namespace = %q labels %v, want %s labelled with its environment", namespace.Metadata.Name, namespace.Metadata.Labels, name)
			}
			ingress := readSNOWorkload[snoWorkloadObject](t, filepath.Join(root, "workloads", "ingress", name+".yaml"))
			if want := name + ".apps.sno." + values.Host.Domain; ingress.Metadata.Name != name || ingress.Spec.Domain != want {
				t.Errorf("ingress controller = %q domain %q, want %s with domain %s", ingress.Metadata.Name, ingress.Spec.Domain, name, want)
			}
			if ingress.Spec.NamespaceSelector.MatchLabels["lab.example.test/environment"] != name {
				t.Errorf("ingress selects %v, want the %s environment", ingress.Spec.NamespaceSelector.MatchLabels, name)
			}
			route := readSNOWorkload[snoWorkloadObject](t, filepath.Join(directory, "app-route.yaml"))
			if want := "app." + name + ".apps.sno." + values.Host.Domain; route.Spec.Host != want {
				t.Errorf("route host = %q, want %q", route.Spec.Host, want)
			}
			for _, file := range []string{"app-deployment.yaml", "probe-pod.yaml"} {
				object := readSNOWorkload[snoWorkloadObject](t, filepath.Join(directory, file))
				annotations := object.Spec.Template.Metadata.Annotations
				if object.Kind == "Pod" {
					annotations = object.Metadata.Annotations
				}
				if annotations["k8s.v1.cni.cncf.io/networks"] != name || object.Metadata.Namespace != name {
					t.Errorf("%s attaches %v in %q, want the %s network in its own namespace", file, annotations, object.Metadata.Namespace, name)
				}
			}
			firewall := readSNOWorkload[snoWorkloadObject](t, filepath.Join(directory, "egressfirewall.yaml"))
			if len(firewall.Spec.Egress) != 1 || firewall.Spec.Egress[0].Type != "Deny" || firewall.Spec.Egress[0].To.CIDRSelector != cidrs[other[name]] {
				t.Errorf("egress firewall = %+v, want one Deny of %s", firewall.Spec.Egress, cidrs[other[name]])
			}
			policy := readSNOWorkload[snoWorkloadEgressPolicy](t, filepath.Join(directory, "egress-networkpolicy.yaml"))
			var except []string
			for _, rule := range policy.Spec.Egress {
				for _, to := range rule.To {
					except = append(except, to.IPBlock.Except...)
				}
			}
			for _, want := range []string{values.Host.CIDR, cidrs[other[name]]} {
				if !slices.Contains(except, want) {
					t.Errorf("egress policy excepts %v, want it to hold %s", except, want)
				}
			}
		})
	}
}
