package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/environment/inspection"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

const keyringFixture = "[client.vm-disks]\n\tkey = AQBx57l5Y2FuYXJ5LWNsaWVudC1rZXk9PQ==\n"

var keyringTargetRefusal = diagnostics.NewFailureWithRemediation("access.target", "the context lab selects no CephClient named missing", "",
	"bootwright cluster info --context lab --name storage lists the CephClients of storage")

func keyringResult(unproved bool) *inspection.KeyringResult {
	return &inspection.KeyringResult{Context: "lab", Cluster: "storage", Client: "vm-disks", Unproved: unproved,
		Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(keyringFixture)})}
}

func runKeyring(result *inspection.KeyringResult, failure error, extra ...string) (int, string, string, *dispatchRecord) {
	record := &dispatchRecord{err: failure}
	record.result.keyring = result
	var out, errOut bytes.Buffer
	args := append([]string{"cluster", "keyring", "--context", "lab", "--name", "storage", "--client", "vm-disks"}, extra...)
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), args)
	return code, out.String(), errOut.String(), record
}

type keyringEnvelope struct {
	OK          bool                `json:"ok"`
	Result      exportResult        `json:"result"`
	Diagnostics []map[string]string `json:"diagnostics"`
}

func TestKeyringExactBytes(t *testing.T) {
	code, out, errOut, record := runKeyring(keyringResult(false), nil)
	if code != 0 || out != keyringFixture || errOut != "" {
		t.Fatalf("a revealed keyring exited %d with %q and standard error %q, want exactly %q", code, out, errOut, keyringFixture)
	}
	if want := (inspection.KeyringRequest{ContextName: "lab", Name: "storage", Client: "vm-disks"}); record.path != "cluster keyring" || record.request != want {
		t.Fatalf("dispatched %s %+v", record.path, record.request)
	}
	for _, failure := range []error{
		keyringTargetRefusal,
		diagnostics.NewFailureWithRemediation("cluster.not-applicable", "bootwright cluster keyring does not apply to edge, an OpenShiftCluster; it applies to managed CephClusters only", "", "bootwright cluster info --context lab lists the clusters this context selects and their kinds"),
		diagnostics.NewFailureWithRemediation("access.unavailable", "this context holds no keyring for CephClient vm-disks of CephCluster storage", "", "bootwright apply --context lab --stage clusters"),
		secretstore.Failure("store.implementation", "this context's secret store is local-keyring-v3, which this Bootwright build cannot open"),
	} {
		code, out, errOut, _ := runKeyring(nil, failure)
		if code != 1 || out != "" || strings.Count(errOut, "  error  ") != 1 {
			t.Fatalf("a refused keyring exited %d with %q and standard error %q, want exit 1, no output and one diagnostic", code, out, errOut)
		}
	}
	for name, material := range map[string]secrets.Material{
		"empty":     {},
		"two parts": secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(keyringFixture), secrets.PasswordPart: []byte("x")}),
		"oversized": secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: bytes.Repeat([]byte("k"), secrets.MaxPartBytes+1)}),
	} {
		code, out, errOut, _ := runKeyring(&inspection.KeyringResult{Context: "lab", Cluster: "storage", Client: "vm-disks", Material: material}, nil)
		if code != 1 || out != "" || !strings.Contains(errOut, "runtime.internal") {
			t.Fatalf("%s keyring exited %d with %q and standard error %q", name, code, out, errOut)
		}
	}
	code, out, errOut, _ = runKeyring(keyringResult(false), nil, "--output", "json")
	var envelope keyringEnvelope
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || code != 0 || errOut != "" || !envelope.OK ||
		envelope.Result.Export != (exportPresentation{MediaType: "text/plain", Encoding: "utf-8", Content: keyringFixture}) || len(envelope.Diagnostics) != 0 {
		t.Fatalf("a JSON keyring exited %d with %q and standard error %q (%v)", code, out, errOut, err)
	}
}

func TestKeyringUnprovedWarning(t *testing.T) {
	code, out, errOut, _ := runKeyring(keyringResult(true), nil)
	wantWarning := "  warning  access.unproved\n" +
		"    the keyring of CephClient vm-disks of CephCluster storage was kept from an apply that never\n" +
		"    proved it, so the access it grants was not proved\n"
	if code != 0 || out != keyringFixture || errOut != wantWarning {
		t.Fatalf("an unproved keyring exited %d with %q and standard error %q, want its bytes and one warning", code, out, errOut)
	}
	code, out, errOut, _ = runKeyring(keyringResult(true), nil, "--output", "json")
	var envelope keyringEnvelope
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || code != 0 || errOut != "" || envelope.Result.Export.Content != keyringFixture ||
		len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0]["code"] != "access.unproved" || envelope.Diagnostics[0]["severity"] != "warning" {
		t.Fatalf("an unproved JSON keyring exited %d with %q and standard error %q (%v)", code, out, errOut, err)
	}
}
