package desiredstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticsSpeakAuthoredFieldVocabulary(t *testing.T) {
	denylist := map[string]string{
		"spec.nodes":        "ContainerCluster hosts live under spec.hosts",
		"topology.nodes":    "StorageCluster hosts live under spec.ceph.topology.hosts",
		"tiebreaker.node":   "the stretch tiebreaker field is tiebreaker.host",
		"networkRefspace":   "the kubevirt network namespace is networkRef.namespace",
		".ip is required":   "bind and resolver entries spell the field address",
		".ip %q":            "bind and resolver entries spell the field address",
		".ip is only valid": "bind and resolver entries spell the field address",
		".endpoint is only": "environment component references spell the field endpointRef",
		".endpoint %q":      "environment component references spell the field endpointRef",
		"endpointRefs":      "cluster install endpoints live under spec.install.endpoints; consumers bind by endpointRef",
		"machineAddress":    "authored machine address references spell the field addressRef",
		"credentialRef":     "secret references spell the field credentialsRef",
	}
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		for needle, authored := range denylist {
			if strings.Contains(text, needle) {
				t.Errorf("%s mentions retired vocabulary %q; %s", path, needle, authored)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
