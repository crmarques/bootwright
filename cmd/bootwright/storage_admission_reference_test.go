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
	sources := exampleSources(t)
	var out, errOut bytes.Buffer
	run(context.Background(), []string{"validate", "-f", sources.Roots[0], "--output", "json"}, &out, &errOut)
	var result struct{ Diagnostics []diagnostics.Diagnostic }
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err, out.String())
	}
	want := map[string]int{"$.spec.ceph.services[3].service_type": 217, "$.spec.ceph.services[4].service_type": 227, "$.spec.ceph.services[5].service_type": 234}
	got := map[string]int{}
	for _, d := range result.Diagnostics {
		if d.Object == nil || d.Object.Kind != "StorageCluster" || !strings.HasSuffix(d.Field, ".service_type") {
			continue
		}
		if d.Source == nil || filepath.Base(filepath.Dir(d.Source.Path)) != "ceph" || filepath.Base(d.Source.Path) != "cluster.yaml" {
			t.Fatalf("service refusal lacks its authored source: %+v", d)
		}
		got[d.Field] = d.Source.Line
	}
	if len(got) != len(want) {
		t.Fatalf("service refusals: got %v want %v", got, want)
	}
	for field, line := range want {
		if got[field] != line {
			t.Fatalf("service refusals: got %v want %v", got, want)
		}
	}
}

func TestReferenceStretchSitesMustBeEnvironmentSites(t *testing.T) {
	const authored = "          - site-b\n        tiebreaker:\n          node: node-07\n          site: site-c\n"
	for _, test := range []struct {
		field, replacement string
		line               int
	}{
		{"$.spec.ceph.topology.stretch.dataSites[1]", "          - site-z\n        tiebreaker:\n          node: node-07\n          site: site-c\n", 72},
		{"$.spec.ceph.topology.stretch.tiebreaker.site", "          - site-b\n        tiebreaker:\n          node: node-07\n          site: site-z\n", 75},
	} {
		t.Run(test.field, func(t *testing.T) {
			sources := exampleSources(t)
			edited := false
			for i, file := range sources.Files {
				if filepath.Base(filepath.Dir(file.Path())) == "ceph" && filepath.Base(file.Path()) == "cluster.yaml" && strings.Count(string(file.Bytes()), authored) == 1 {
					sources.Files[i] = desiredstate.NewSourceFile(file.Path(), []byte(strings.Replace(string(file.Bytes()), authored, test.replacement, 1)))
					edited = true
				}
			}
			if !edited {
				t.Fatal("reference stretch declaration moved")
			}
			_, _, err := wireCompiler().Compile(context.Background(), sources)
			for _, d := range diagnostics.Of(err) {
				if d.Field == test.field && d.Message == "storage site must resolve to a declared Environment site" && d.Source != nil && d.Source.Line == test.line {
					return
				}
			}
			t.Fatalf("undeclared site was not refused at its source: %+v", diagnostics.Of(err))
		})
	}
}
