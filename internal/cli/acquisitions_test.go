package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestTheAcquisitionListIsOneSortedTableOrOneResult(t *testing.T) {
	list := func(api.Catalog) ([]Acquisition, error) {
		return []Acquisition{
			{Kind: "service image", Artifact: "docker.io/ubuntu/squid", Version: "sha256:" + strings.Repeat("a", 64), Origin: "docker.io/ubuntu/squid", Class: "public", SHA256: strings.Repeat("a", 64), Machines: []string{"b", "a", "a"}, Consumer: "Proxy/egress"},
			{Kind: "client", Artifact: "helm", Version: "4.3.1", Origin: "https://get.helm.sh/helm.tar.gz", Class: "public", Bytes: 10, Machines: []string{"controller"}},
		}, nil
	}
	var text bytes.Buffer
	if err := writeAcquisitions(&text, "render acquisitions", list, api.Catalog{}, false); err != nil {
		t.Fatal(err)
	}
	listed := text.String()
	helm, squid := strings.Index(listed, "acquisition | helm\n"), strings.Index(listed, "acquisition | docker.io/ubuntu/squid\n")
	if helm < 0 || squid < helm || !strings.Contains(listed[helm:squid], "SIZE         10 B\n") || !strings.Contains(listed[squid:], "MACHINES     a,b\n") || !strings.Contains(listed[squid:], "SELECTED BY  Proxy/egress\n") {
		t.Fatalf("table:\n%s", listed)
	}
	var structured bytes.Buffer
	if err := writeAcquisitions(&structured, "render acquisitions", list, api.Catalog{}, true); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Result acquisitionsPresentation `json:"result"`
	}
	if err := json.Unmarshal(structured.Bytes(), &envelope); err != nil || len(envelope.Result.Acquisitions) != 2 || envelope.Result.Acquisitions[0].Artifact != "helm" {
		t.Fatalf("result: %v %s", err, structured.String())
	}
	if err := writeAcquisitions(&text, "render acquisitions", nil, api.Catalog{}, false); err == nil {
		t.Fatal("an executable with no list wrote one")
	}
	failure := errors.New("selection failed")
	if err := writeAcquisitions(&text, "render acquisitions", func(api.Catalog) ([]Acquisition, error) { return nil, failure }, api.Catalog{}, false); !errors.Is(err, failure) {
		t.Fatalf("a failed list returned %v", err)
	}
}
