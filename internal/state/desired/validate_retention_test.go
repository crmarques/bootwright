package desiredstate

import (
	"strings"
	"testing"
)

func TestArtifactServerRetentionValidation(t *testing.T) {
	t.Run("bogus-retention-rejected", func(t *testing.T) {
		err := loadWithInfraComponent(t, strings.Replace(newInfraComponentYAML,
			"    machineRef: services-host\n",
			"    machineRef: services-host\n    retention: bogus\n", 1))
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}
		if want := `spec.artifactServer.retention "bogus" must be one of {persistent, install-only}`; !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
	})
	t.Run("install-only-accepted", func(t *testing.T) {
		if err := loadWithInfraComponent(t, strings.Replace(newInfraComponentYAML,
			"    machineRef: services-host\n",
			"    machineRef: services-host\n    retention: install-only\n", 1)); err != nil {
			t.Fatalf("unexpected validation error: %v", err)
		}
	})
	t.Run("persistent-accepted", func(t *testing.T) {
		if err := loadWithInfraComponent(t, strings.Replace(newInfraComponentYAML,
			"    machineRef: services-host\n",
			"    machineRef: services-host\n    retention: persistent\n", 1)); err != nil {
			t.Fatalf("unexpected validation error: %v", err)
		}
	})
}

func loadWithInfraComponent(t *testing.T, infraComponentYAML string) error {
	t.Helper()
	dir := t.TempDir()
	files := newBaselineFiles()
	files["infra-component.yaml"] = infraComponentYAML
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	return err
}
