//go:build linux && amd64

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/storage/ceph"
)

// The client entry an apply captures is the canonical JSON of the one
// credential ceph auth get-or-create returned for the client; the export
// renders it in the keyring form that command prints.
func TestClusterKeyringRevealsOneClientThroughCustody(t *testing.T) {
	services, repository := cephExampleContext(t, "lab-ceph")
	entry := `[{"caps":{"mgr":"profile rbd pool=rbd-data","mon":"profile rbd","osd":"profile rbd pool=rbd-data"},"entity":"client.rbd-canary","key":"` + clientKeyToken + `"}]`
	produceThroughLentArea(t, services, repository, "lab-ceph", ceph.ClientBlockID("rbd-canary"), ceph.ClientOutput, entry)
	out, errOut := contextRun(t, services, 0, "cluster", "keyring", "--context", "lab-ceph", "--name", "ceph-lab", "--client", "rbd-canary")
	if out != clientCanary || errOut != "" {
		t.Fatalf("keyring = %q, standard error %q, want exactly %q", out, errOut, clientCanary)
	}
	out, errOut = contextRun(t, services, 1, "cluster", "keyring", "--context", "lab-ceph", "--name", "ceph-lab", "--client", "rbd-missing")
	if out != "" || !strings.Contains(errOut, "access.target") || !strings.Contains(errOut, "selects no CephClient named rbd-missing") || strings.Contains(errOut, clientKeyToken) {
		t.Fatalf("an unknown client wrote %q and %q", out, errOut)
	}
	out, errOut = contextRun(t, services, 0, "cluster", "keyring", "--context", "lab-ceph", "--name", "ceph-lab", "--client", "rbd-canary", "--output", "json")
	var envelope struct {
		Result struct {
			Export struct {
				MediaType string `json:"mediaType"`
				Encoding  string `json:"encoding"`
				Content   string `json:"content"`
			} `json:"export"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || errOut != "" || envelope.Result.Export.MediaType != "text/plain" ||
		envelope.Result.Export.Encoding != "utf-8" || envelope.Result.Export.Content != clientCanary {
		t.Fatalf("a JSON keyring = %q, standard error %q (%v)", out, errOut, err)
	}
}
