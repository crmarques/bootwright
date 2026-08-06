package repocheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type nativeCatalogAddOn struct {
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Accepts struct {
			Inputs []struct {
				Name        string `yaml:"name"`
				ResourceRef *struct {
					Kind string `yaml:"kind"`
				} `yaml:"resourceRef"`
			} `yaml:"inputs"`
		} `yaml:"accepts"`
		Steps []struct {
			Name      string `yaml:"name"`
			Playbook  string `yaml:"playbook"`
			Manifests []struct {
				Path string `yaml:"path"`
			} `yaml:"manifests"`
		} `yaml:"steps"`
	} `yaml:"spec"`
}

func loadNativeCatalogAddOns(t *testing.T) map[string]nativeCatalogAddOn {
	t.Helper()
	root := filepath.Join(repoRoot(t), "add-ons")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read add-ons dir: %v", err)
	}
	out := map[string]nativeCatalogAddOn{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		versions, err := os.ReadDir(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatalf("read add-ons/%s: %v", entry.Name(), err)
		}
		for _, version := range versions {
			if !version.IsDir() {
				continue
			}
			addOnPath := filepath.Join(root, entry.Name(), version.Name(), "add-on.yaml")
			data, err := os.ReadFile(addOnPath)
			if err != nil {
				continue
			}
			var addon nativeCatalogAddOn
			if err := yaml.Unmarshal(data, &addon); err != nil {
				t.Fatalf("parse %s: %v", addOnPath, err)
			}
			key := entry.Name() + "/" + version.Name()
			out[key] = addon
		}
	}
	if len(out) == 0 {
		t.Fatal("no add-ons/*/*/add-on.yaml entries found")
	}
	return out
}

var stepRefDotToken = regexp.MustCompile(`bootwright_step_refs\.([A-Za-z_][A-Za-z0-9_]*)`)
var stepRefBracketToken = regexp.MustCompile(`bootwright_step_refs\[['"]([^'"]+)['"]\]`)

func TestNativeCatalogStepRefTokensResolveToAcceptedInputs(t *testing.T) {
	root := repoRoot(t)
	for key, addon := range loadNativeCatalogAddOns(t) {
		refInputs := map[string]bool{}
		for _, input := range addon.Spec.Accepts.Inputs {
			if input.ResourceRef != nil {
				refInputs[input.Name] = true
			}
		}
		for _, step := range addon.Spec.Steps {
			if step.Playbook == "" {
				continue
			}
			playbookPath := filepath.Join(root, "add-ons", strings.SplitN(key, "/", 2)[0], strings.SplitN(key, "/", 2)[1], step.Playbook)
			data, err := os.ReadFile(playbookPath)
			if err != nil {
				t.Fatalf("%s: read step %q playbook %s: %v", key, step.Name, step.Playbook, err)
			}
			text := string(data)
			seen := map[string]bool{}
			for _, m := range stepRefDotToken.FindAllStringSubmatch(text, -1) {
				seen[m[1]] = true
			}
			for _, m := range stepRefBracketToken.FindAllStringSubmatch(text, -1) {
				seen[m[1]] = true
			}
			for name := range seen {
				if !refInputs[name] {
					t.Errorf("%s: step %q playbook %s references bootwright_step_refs key %q, which is not a resourceRef-typed accepted input (valid keys: %v)", key, step.Name, step.Playbook, name, sortedKeys(refInputs))
				}
			}
		}
	}
}

func nativeCatalogStepsYAML(t *testing.T, addOnDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(addOnDir, "add-on.yaml"))
	if err != nil {
		t.Fatalf("read %s/add-on.yaml: %v", addOnDir, err)
	}
	var document struct {
		Spec struct {
			Steps yaml.Node `yaml:"steps"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("parse %s/add-on.yaml: %v", addOnDir, err)
	}
	encoded, err := yaml.Marshal(&document.Spec.Steps)
	if err != nil {
		t.Fatalf("encode %s spec.steps: %v", addOnDir, err)
	}
	return string(encoded)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestNativeCatalogODFFDFStepContentParity(t *testing.T) {
	root := repoRoot(t)
	odf := filepath.Join(root, "add-ons", "openshift-data-foundation", "4.21")
	fdf := filepath.Join(root, "add-ons", "fusion-data-foundation", "4.21")
	if _, err := os.Stat(odf); err != nil {
		t.Skipf("openshift-data-foundation/4.21 not present: %v", err)
	}
	if _, err := os.Stat(fdf); err != nil {
		t.Skipf("fusion-data-foundation/4.21 not present: %v", err)
	}

	identical := []string{
		"playbooks/export-external-details.yaml",
		"manifests/ocs-external-storagecluster.yaml",
	}
	for _, rel := range identical {
		a, err := os.ReadFile(filepath.Join(odf, rel))
		if err != nil {
			t.Fatalf("read openshift-data-foundation %s: %v", rel, err)
		}
		b, err := os.ReadFile(filepath.Join(fdf, rel))
		if err != nil {
			t.Fatalf("read fusion-data-foundation %s: %v", rel, err)
		}
		if string(a) != string(b) {
			t.Errorf("openshift-data-foundation and fusion-data-foundation %s have drifted apart; they are documented as sharing identical step content (ADR 0013) -- if the divergence is deliberate, update this test's expectations", rel)
		}
	}

	odfSteps := nativeCatalogStepsYAML(t, odf)
	fdfSteps := nativeCatalogStepsYAML(t, fdf)
	if odfSteps != fdfSteps {
		t.Errorf("openshift-data-foundation and fusion-data-foundation spec.steps have drifted apart; they are documented as sharing identical step content (ADR 0013) -- if the divergence is deliberate, update this test's expectations\n  odf:\n%s\n  fdf:\n%s", odfSteps, fdfSteps)
	}

	rel := "manifests/rook-ceph-external-cluster-details.yaml"
	a, err := os.ReadFile(filepath.Join(odf, rel))
	if err != nil {
		t.Fatalf("read openshift-data-foundation %s: %v", rel, err)
	}
	b, err := os.ReadFile(filepath.Join(fdf, rel))
	if err != nil {
		t.Fatalf("read fusion-data-foundation %s: %v", rel, err)
	}
	aLines := strings.Split(string(a), "\n")
	bLines := strings.Split(string(b), "\n")
	if len(aLines) != len(bLines) {
		t.Fatalf("openshift-data-foundation and fusion-data-foundation %s have a different line count (%d vs %d); expected only the bootwright.io/addon annotation line to differ", rel, len(aLines), len(bLines))
	}
	for i := range aLines {
		if aLines[i] == bLines[i] {
			continue
		}
		if strings.Contains(aLines[i], "bootwright.io/addon:") && strings.Contains(bLines[i], "bootwright.io/addon:") {
			continue
		}
		t.Errorf("openshift-data-foundation and fusion-data-foundation %s differ at line %d beyond the known bootwright.io/addon annotation:\n  odf: %q\n  fdf: %q", rel, i+1, aLines[i], bLines[i])
	}
}

func TestNativeCatalogDataFoundationGatewayConditionalIsBoolean(t *testing.T) {
	root := repoRoot(t)
	for _, addon := range []string{"openshift-data-foundation", "fusion-data-foundation"} {
		path := filepath.Join(root, "add-ons", addon, "4.21", "playbooks", "export-external-details.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(data), "when: bootwright_gateway is not none") {
			t.Errorf("%s must test gateway presence with a boolean expression", path)
		}
	}
}

var addonAnnotationLine = regexp.MustCompile(`bootwright\.io/addon:\s*(\S+)`)

func TestNativeCatalogManifestAddonAnnotationMatchesOwnName(t *testing.T) {
	root := repoRoot(t)
	for key, addon := range loadNativeCatalogAddOns(t) {
		if addon.Metadata.Name == "" {
			t.Fatalf("%s: add-on.yaml has no metadata.name", key)
		}
		dir := filepath.Join(root, "add-ons", strings.SplitN(key, "/", 2)[0], strings.SplitN(key, "/", 2)[1])
		for _, step := range addon.Spec.Steps {
			for _, manifest := range step.Manifests {
				data, err := os.ReadFile(filepath.Join(dir, manifest.Path))
				if err != nil {
					t.Fatalf("%s: read manifest %s: %v", key, manifest.Path, err)
				}
				for _, m := range addonAnnotationLine.FindAllStringSubmatch(string(data), -1) {
					if m[1] != addon.Metadata.Name {
						t.Errorf("%s: manifest %s has bootwright.io/addon: %s, which does not match this add-on's own metadata.name %s", key, manifest.Path, m[1], addon.Metadata.Name)
					}
				}
			}
		}
	}
}
