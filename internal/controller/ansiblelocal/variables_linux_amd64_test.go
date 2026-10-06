//go:build linux && amd64

package ansiblelocal

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// controllerFixture is what the collection's units suite hands ansible-playbook
// as this runner's request.json, so the Python proof reads the bytes this
// runner writes.
const controllerFixture = "../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens/extra_vars/controller.json"

// templatedSetup carries remote package metadata and authored egress that each
// hold a delimiter ansible-core would render.
func templatedSetup() capabilityRequest {
	sha := strings.Repeat("a", 64)
	source := prerequisites.DependencySource{ID: "native-one", URL: "https://packages.example.test/el9/chrony.rpm", SHA256: sha, Bytes: 9007199254740993}
	return capabilityRequest{
		Version: requestVersion, Operation: "setup", Identity: sha,
		Platform:          prerequisites.Platform{OS: "rhel", Release: "9.8", Architecture: "x86_64"},
		Bundle:            bundleLocation{Path: "/bundle", Device: 2049, Inode: 18446744073709551615, Writable: true},
		PublicationBundle: bundleLocation{Path: "/bundle", Device: 2049, Inode: 18446744073709551615, Writable: true},
		Packages:          []prerequisites.NativePackage{{Name: "{{ lookup('pipe', 'id') }}", Version: "4.6", Release: "1.el9", Architecture: "x86_64", Signer: sha, Source: source}},
		Tools:             []prerequisites.ToolDefinition{},
		Acquisition:       []toolAcquisition{},
		Egress:            prerequisites.SetupEgress{HTTPProxy: "http://proxy.example.test:8080/{% raw %}kept{% endraw %}", NoProxy: []string{"{{ 7*6 }}"}},
	}
}

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

// Setup's own request carries remote package metadata and the controller's
// authored egress, and ansible-core reads an --extra-vars file as trusted
// template text, so every string under the request is an __ansible_unsafe
// object; the inventory, whose values the runner chooses, stays plain JSON.
func TestTheControllerRunnerWritesEveryRequestStringAsData(t *testing.T) {
	job := t.TempDir()
	request := templatedSetup()
	if err := writeInvocation(job, prerequisites.PythonLaunch{Loader: "/qualified/loader"}, request); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(filepath.Join(job, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := json.Marshal(map[string]any{"bootwright_controller_request": request})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := unmarked(t, decodeExact(t, written), "$"), decodeExact(t, plain); !reflect.DeepEqual(got, want) {
		t.Fatalf("Ansible reads %v, not the request %v", got, want)
	}
	inventory, err := os.ReadFile(filepath.Join(job, "inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"all":{"children":{"bootwright_controller":{"hosts":{"controller":{"ansible_connection":"local","ansible_host":"localhost","ansible_python_interpreter":` + string(mustQuote(t, filepath.Join(job, "interpreter"))) + `}}}}}}`
	if string(inventory) != want {
		t.Fatalf("the inventory changed form:\n%s\nwant\n%s", inventory, want)
	}
}

// The collection's units suite runs ansible-playbook over this fixture to prove
// a marked string reaches a module verbatim, so the fixture is exactly what
// this runner writes for a request holding every delimiter.
func TestTheControllerRunnerWritesTheCollectionsFixture(t *testing.T) {
	job := t.TempDir()
	if err := writeInvocation(job, prerequisites.PythonLaunch{Loader: "/qualified/loader"}, templatedSetup()); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(filepath.Join(job, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(controllerFixture)
	if err != nil {
		t.Fatal(err)
	}
	if want := append(written, '\n'); !bytes.Equal(fixture, want) {
		t.Fatalf("%s holds\n%s\nbut the runner writes\n%s", controllerFixture, fixture, want)
	}
}

func mustQuote(t *testing.T, text string) []byte {
	t.Helper()
	quoted, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	return quoted
}
