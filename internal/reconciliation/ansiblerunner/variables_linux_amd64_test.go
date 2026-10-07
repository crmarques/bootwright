//go:build linux && amd64

package ansiblerunner

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

// templatedRequest holds every delimiter ansible-core would render, in a
// value, a list item and a nested value, and a number a float64 would round.
const templatedRequest = `{"kickstart":"%packages\n{{ 7*6 }}\n%end\n","names":["{{ lookup('pipe', 'id') }}","{% raw %}kept{% endraw %}"],"nested":{"comment":"{# x #}"},"port":8443,"serial":9007199254740993}`

// lifecycleFixture is what the collection's units suite hands ansible-playbook
// as this runner's request.json, so the Python proof reads the bytes this
// runner writes.
const lifecycleFixture = "../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens/extra_vars/lifecycle.json"

func decodeExact(t *testing.T, data []byte) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("the document does not decode: %v\n%s", err, data)
	}
	return decoded
}

// unmarked undoes the runner's marking, failing on any string that is not the
// one member of an __ansible_unsafe object.
func unmarked(t *testing.T, value any, path string) any {
	t.Helper()
	switch typed := value.(type) {
	case string:
		t.Fatalf("%s is the plain string %q, which ansible-core would read as a template", path, typed)
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = unmarked(t, item, path+"[]")
		}
		return out
	case map[string]any:
		if text, found := typed["__ansible_unsafe"]; found {
			if _, isString := text.(string); !isString || len(typed) != 1 {
				t.Fatalf("%s is not an object whose one member is __ansible_unsafe: %v", path, typed)
			}
			return text
		}
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = unmarked(t, item, path+"."+key)
		}
		return out
	}
	return value
}

// requestReader starts an adapter that completes at once, keeping the job it
// ran in and the exact bytes of its request.json.
func requestReader(t *testing.T, job *string, written *[]byte) Runner {
	runner := sweepingRunner(t, nil)
	runner.command = func(_ string, arguments ...string) *exec.Cmd {
		for _, argument := range arguments {
			if value, found := strings.CutPrefix(argument, "@"); found {
				*job = filepath.Dir(value)
				data, err := os.ReadFile(value)
				if err != nil {
					t.Errorf("the adapter's request.json does not read: %v", err)
				}
				*written = data
			}
		}
		return exec.Command("/bin/sh", "-c", `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4
printf '{"evidence":{"absent":false},"outcome":"changed","phase":"completed"}\n' >&3`)
	}
	return runner
}

// ansible-core reads an --extra-vars file as trusted template text, so every
// string the adapter reads, from the frozen request, its digest, its material
// and its outputs alike, is an __ansible_unsafe object, and unmarking them
// gives back exactly the variables, every number in its canonical text.
func TestTheAdapterReadsEveryRequestStringAsData(t *testing.T) {
	var job string
	var written []byte
	runner := requestReader(t, &job, &written)
	var output bytes.Buffer
	request := adapterRequest(t, &output)
	request.Canonical = []byte(templatedRequest)
	request.Materials = []lifecycle.MaterialFile{{Name: "tls.crt", Part: secrets.CertificatePart, Secret: "artifact-server-tls", Variable: "certificate"}}
	request.Material = map[string]secrets.Material{"artifact-server-tls": secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: []byte("CERTIFICATE")})}
	request.MaterialValues = map[string]string{"fingerprint": "{{ 6*7 }}"}
	request.Outputs = []lifecycle.OutputFile{{Name: "kubeconfig", Variable: "kubeconfig"}}
	if _, err := runner.Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	values, err := variables(request, map[string]string{"tls.crt": filepath.Join(job, "tls.crt")})
	if err != nil {
		t.Fatal(err)
	}
	values["bootwright_artifact_server_output"] = map[string]any{"kubeconfig": filepath.Join(job, outputsDirectory, "kubeconfig")}
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := unmarked(t, decodeExact(t, written), "$"), decodeExact(t, encoded); !reflect.DeepEqual(got, want) {
		t.Fatalf("the adapter read %v, not the variables %v", got, want)
	}
	if !bytes.Contains(written, []byte(`"serial":9007199254740993`)) {
		t.Fatalf("a number lost its canonical text: %s", written)
	}
}

// The collection's units suite runs ansible-playbook over this fixture to prove
// a marked string reaches a module verbatim, so the fixture is exactly what
// Run writes for a request holding every delimiter.
func TestTheLifecycleRunnerWritesTheCollectionsFixture(t *testing.T) {
	request := lifecycle.RunRequest{
		Context: "fixture-context", Block: "fixture-block", Description: "Serve the {{ fixture }}",
		Variable: "bootwright_fixture", Digest: "sha256:" + strings.Repeat("5f", 32), Canonical: []byte(templatedRequest),
		Materials:      []lifecycle.MaterialFile{{Name: "tls.crt", Part: secrets.CertificatePart, Secret: "fixture-tls", Variable: "certificate"}},
		MaterialValues: map[string]string{"fingerprint": "{{ 6*7 }}"},
	}
	values, err := variables(request, map[string]string{"tls.crt": "/job/tls.crt"})
	if err != nil {
		t.Fatal(err)
	}
	values["bootwright_fixture_output"] = map[string]any{"kubeconfig": "/job/outputs/kubeconfig"}
	job := t.TempDir()
	if err := writeVariables(job, values); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(filepath.Join(job, requestName))
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(lifecycleFixture)
	if err != nil {
		t.Fatal(err)
	}
	if want := append(written, '\n'); !bytes.Equal(fixture, want) {
		t.Fatalf("%s holds\n%s\nbut the runner writes\n%s", lifecycleFixture, fixture, want)
	}
}

// A frozen request is one JSON object, so null, any other value and an object
// followed by more text are each refused before the adapter is handed anything.
func TestARequestThatIsNotOneObjectIsNotPrepared(t *testing.T) {
	for _, canonical := range []string{`null`, `[]`, `"{}"`, `{} {}`, `{"a":1}null`, `{`} {
		request := localRequest()
		request.Canonical = []byte(canonical)
		if values, err := variables(request, nil); err == nil {
			t.Fatalf("%s was prepared as %v", canonical, values)
		} else if code, _ := codeOf(err); code != "lifecycle.state" {
			t.Fatalf("%s was refused as %v", canonical, err)
		}
	}
}

// Marking grows the smallest string a request holds, "", from three bytes to
// twenty-four, so a canonical request at its bound still fits the variables
// bound the runner refuses beyond.
func TestTheLargestRequestStillFitsTheVariablesBound(t *testing.T) {
	const prefix, suffix = `{"a":[`, `""]}`
	canonical := prefix + strings.Repeat(`"",`, (reconciliation.MaxRequestBytes-len(prefix)-len(suffix))/3) + suffix
	if len(canonical) != reconciliation.MaxRequestBytes {
		t.Fatalf("the request is %d bytes, not the %d bound", len(canonical), reconciliation.MaxRequestBytes)
	}
	var job string
	var written []byte
	runner := requestReader(t, &job, &written)
	var output bytes.Buffer
	request := adapterRequest(t, &output)
	request.Canonical = []byte(canonical)
	if _, err := runner.Run(context.Background(), request); err != nil {
		t.Fatalf("the largest request was refused: %v", err)
	}
	if len(written) < 7*reconciliation.MaxRequestBytes || len(written) > maxVariableBytes {
		t.Fatalf("the adapter read %d bytes of variables", len(written))
	}
}
