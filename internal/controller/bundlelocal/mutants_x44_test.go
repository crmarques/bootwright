package bundlelocal

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A wheel report that resolves every other dependency but leaves out
// ansible-core or urllib3 is refused, even though each wheel it does name is
// an approved source: the bootstrap cannot run without either (X33's mutants,
// B255).
func TestAPipReportWithoutARequiredBootstrapDependencyIsRefused(t *testing.T) {
	item := func(name, version string) map[string]any {
		return map[string]any{"metadata": map[string]string{"name": name, "version": version}, "download_info": map[string]any{
			"url": "https://files.pythonhosted.org/packages/" + name + ".whl", "archive_info": map[string]any{"hashes": map[string]string{"sha256": strings.Repeat("b", 64)}}}}
	}
	for name, install := range map[string][]map[string]any{
		"without ansible-core": {item("urllib3", "2.7.0"), item("pyyaml", "6.0.3")},
		"without urllib3":      {item("ansible-core", "2.21.4"), item("pyyaml", "6.0.3")},
	} {
		t.Run(name, func(t *testing.T) {
			data, _ := json.Marshal(map[string]any{"version": "1", "install": install, "environment": map[string]string{
				"python_full_version": "3.14.7", "implementation_name": "cpython", "platform_machine": "x86_64", "sys_platform": "linux"}})
			wheels, sources, err := parsePipReport(data, prerequisites.BootstrapDefinition{AnsibleVersion: "2.21.4", PythonVersion: "3.14.7"})
			found := diagnostics.Of(err)
			if len(found) != 1 || !strings.Contains(found[0].Message, "omitted a required bootstrap dependency") || len(wheels) != 0 || len(sources) != 0 {
				t.Fatalf("the report was accepted: %v, %v, %+v", wheels, sources, found)
			}
		})
	}
}
