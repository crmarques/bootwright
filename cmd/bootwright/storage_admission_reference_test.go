package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestNativeServiceRefusalsPointAtTheAuthoredService(t *testing.T) {
	const authored = "      - service_type: mds\n        service_id: odf-cephfs\n"
	sources := exampleSources(t)
	var edited []byte
	for i, file := range sources.Files {
		if filepath.Base(filepath.Dir(file.Path())) == "ceph" && filepath.Base(file.Path()) == "cluster.yaml" && strings.Count(string(file.Bytes()), authored) == 1 {
			edited = []byte(strings.Replace(string(file.Bytes()), authored, "      - service_type: jaeger-agent\n\n        placement:\n          label: mon\n\n"+authored, 1))
			sources.Files[i] = desiredstate.NewSourceFile(file.Path(), edited)
		}
	}
	if edited == nil {
		t.Fatal("reference MDS service declaration moved")
	}
	_, _, err := wireCompiler().Compile(context.Background(), sources)
	found := []diagnostics.Diagnostic{}
	for _, d := range diagnostics.Of(err) {
		if d.Object != nil && d.Object.Kind == "StorageCluster" && strings.HasSuffix(d.Field, ".service_type") {
			found = append(found, d)
		}
	}
	line := authoredStorageLine(t, edited, "- service_type: jaeger-agent")
	if len(found) != 1 || found[0].Field != "$.spec.ceph.services[6].service_type" || found[0].Message != "Bootwright does not realize tracing services" || found[0].Source == nil || found[0].Source.Line != line || filepath.Base(found[0].Source.Path) != "cluster.yaml" {
		t.Fatalf("service refusals: %+v want line %d", found, line)
	}
}

func TestMultidcExampleMigratesWithoutLegacyFields(t *testing.T) {
	sources := exampleSources(t)
	var out, errOut bytes.Buffer
	run(context.Background(), []string{"validate", "-f", sources.Roots[0], "--output", "json"}, &out, &errOut)
	var result struct{ Diagnostics []diagnostics.Diagnostic }
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err, out.String())
	}
	for _, d := range result.Diagnostics {
		if d.Object != nil && d.Object.Kind == "StorageFilesystem" || d.Source != nil && filepath.Base(d.Source.Path) == "odf-cephfs.yaml" {
			t.Fatalf("the reference filesystem is refused: %+v", d)
		}
	}
	filesystems, services := 0, 0
	for _, file := range sources.Files {
		text := string(file.Bytes())
		switch {
		case filepath.Base(file.Path()) == "odf-cephfs.yaml":
			filesystems++
			if _, mds, _ := strings.Cut(text, "\n  mds:\n"); strings.Contains(mds, "placement:") || !strings.Contains(text, "\n  mdsServiceRef: mds.odf-cephfs\n") {
				t.Fatalf("the reference filesystem keeps a legacy MDS field:\n%s", text)
			}
		case filepath.Base(filepath.Dir(file.Path())) == "ceph" && filepath.Base(file.Path()) == "cluster.yaml":
			services = strings.Count(text, "- service_type: mds\n")
			if strings.Count(text, "- service_type: mds\n        service_id: odf-cephfs\n") != 1 {
				t.Fatal("the reference cluster does not declare mds.odf-cephfs")
			}
		}
	}
	if filesystems != 1 || services != 1 {
		t.Fatalf("reference filesystems %d, MDS services %d", filesystems, services)
	}
}

func TestReferenceStretchSitesMustBeEnvironmentSites(t *testing.T) {
	const authored = "          - site-b\n\n        tiebreaker:\n          node: node-07\n          site: site-c\n"
	for _, test := range []struct {
		field, replacement, changedLine string
	}{
		{"$.spec.ceph.topology.stretch.dataSites[1]", strings.Replace(authored, "site-b", "site-z", 1), "- site-z"},
		{"$.spec.ceph.topology.stretch.tiebreaker.site", strings.Replace(authored, "site-c", "site-z", 1), "site: site-z"},
	} {
		t.Run(test.field, func(t *testing.T) {
			sources := exampleSources(t)
			edited := false
			var line int
			for i, file := range sources.Files {
				if filepath.Base(filepath.Dir(file.Path())) == "ceph" && filepath.Base(file.Path()) == "cluster.yaml" && strings.Count(string(file.Bytes()), authored) == 1 {
					sources.Files[i] = desiredstate.NewSourceFile(file.Path(), []byte(strings.Replace(string(file.Bytes()), authored, test.replacement, 1)))
					line = authoredStorageLine(t, sources.Files[i].Bytes(), test.changedLine)
					edited = true
				}
			}
			if !edited {
				t.Fatal("reference stretch declaration moved")
			}
			_, _, err := wireCompiler().Compile(context.Background(), sources)
			for _, d := range diagnostics.Of(err) {
				if d.Field == test.field && d.Message == "storage site must resolve to a declared Environment site" && d.Source != nil && d.Source.Line == line {
					return
				}
			}
			t.Fatalf("undeclared site was not refused at its source: %+v", diagnostics.Of(err))
		})
	}
}

func authoredStorageLine(t *testing.T, data []byte, authored string) int {
	t.Helper()
	found := 0
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == authored {
			if found != 0 {
				t.Fatalf("authored storage line %q is not unique", authored)
			}
			found = i + 1
		}
	}
	if found == 0 {
		t.Fatalf("authored storage line %q is missing", authored)
	}
	return found
}
