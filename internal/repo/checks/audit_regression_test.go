package repocheck

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func walkAnsibleTaskFiles(t *testing.T, fn func(rel string)) {
	t.Helper()
	root := filepath.Join(repoRoot(t), filepath.FromSlash(bootwrightCollectionRoleRoot))
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".yml" || !strings.Contains(path, string(filepath.Separator)+"tasks"+string(filepath.Separator)) {
			return nil
		}
		rel, err := filepath.Rel(repoRoot(t), path)
		if err != nil {
			return err
		}
		fn(filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk ansible roles: %v", err)
	}
}

func assertPatchThenSurface(t *testing.T, rel string, tasks []map[string]any, patchName, registerVar, surfaceName, surfaceModule string) {
	t.Helper()

	patchIdx := findAnsibleTaskIndex(tasks, patchName)
	if patchIdx < 0 {
		t.Fatalf("%s: missing tolerated cert-verification task %q", rel, patchName)
	}
	if got, _ := tasks[patchIdx]["register"].(string); got != registerVar {
		t.Fatalf("%s: task %q must register %q (got %q); the surfacing task keys off this variable", rel, patchName, registerVar, got)
	}

	surfaceIdx := findAnsibleTaskIndex(tasks, surfaceName)
	if surfaceIdx < 0 {
		t.Fatalf("%s: missing surfacing task %q after %q; a swallowed cert-verification refusal would go unreported", rel, surfaceName, patchName)
	}
	if surfaceIdx <= patchIdx {
		t.Fatalf("%s: surfacing task %q (idx %d) must run AFTER the tolerated %q (idx %d)", rel, surfaceName, surfaceIdx, patchName, patchIdx)
	}
	if _, ok := tasks[surfaceIdx][surfaceModule]; !ok {
		t.Fatalf("%s: surfacing task %q must use %s", rel, surfaceName, surfaceModule)
	}
	if body := fmt.Sprint(tasks[surfaceIdx]); !strings.Contains(body, registerVar+".status") {
		t.Fatalf("%s: surfacing task %q must reference the tolerated call's result %s.status so a privilege refusal surfaces", rel, surfaceName, registerVar)
	}
}

func TestRedfishCertVerificationRefusalsSurface(t *testing.T) {
	root := bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks"

	insertAttempt := readAnsibleTasks(t, root+"/media/insert_attempt.yml")
	assertPatchThenSurface(t, "media/insert_attempt.yml", insertAttempt,
		"Disable HTTPS certificate verification for virtual-media fetch",
		"bootwright_redfish_vmedia_verify_certificate_patch",
		"Confirm the virtual-media certificate verification disable was not refused for lack of privilege",
		"ansible.builtin.assert",
	)

	restore := readAnsibleTasks(t, root+"/media/restore_certificate_verification.yml")
	assertPatchThenSurface(t, "media/restore_certificate_verification.yml", restore,
		"Restore virtual media fetch certificate verification",
		"bootwright_redfish_vmedia_verify_certificate_restore",
		"Warn if virtual media fetch certificate verification was not restored",
		"ansible.builtin.debug",
	)
	assertPatchThenSurface(t, "media/restore_certificate_verification.yml", restore,
		"Restore Redfish HTTPS transfer certificate verification",
		"bootwright_redfish_security_service_https_transfer_restore",
		"Warn if HTTPS transfer certificate verification was not restored",
		"ansible.builtin.debug",
	)

	std := readAnsibleTasks(t, root+"/media/import_certificate/standard.yml")
	importName := "Import the artifact certificate via the DMTF VirtualMedia store"
	importIdx := findAnsibleTaskIndex(std, importName)
	if importIdx < 0 {
		t.Fatalf("import_certificate/standard.yml: missing import POST %q", importName)
	}
	if fw, ok := std[importIdx]["failed_when"]; !ok || fw != false {
		t.Fatalf("import_certificate/standard.yml: import POST %q must carry failed_when:false so the bare 403 is tolerated and re-surfaced by the following assert (got %v, present=%v)", importName, fw, ok)
	}
	assertPatchThenSurface(t, "media/import_certificate/standard.yml", std,
		importName,
		"bootwright_redfish_artifact_cert_import_standard",
		"Confirm the DMTF VirtualMedia certificate import was accepted",
		"ansible.builtin.assert",
	)
}

func TestValidateCertsFalseIsAllowlisted(t *testing.T) {
	allowed := map[string]bool{
		bootwrightCollectionRoleRoot + "/container_cluster_agent_install/tasks/iso/publish_target.yml": true,
		bootwrightCollectionRoleRoot + "/infra_component_artifact_server_http/tasks/main.yml":          true,
	}

	literalFalse := regexp.MustCompile(`validate_certs:\s*false\b`)
	found := map[string]bool{}
	walkAnsibleTaskFiles(t, func(rel string) {
		if !literalFalse.MatchString(readRepoFile(t, rel)) {
			return
		}
		found[rel] = true
		if !allowed[rel] {
			t.Errorf("validate_certs: false in non-allowlisted task file %s; an unconditional TLS-verification disable is only accepted in the managed self-signed artifact-server reachability probes (every other site must gate it on an operator flag)", rel)
		}
	})
	for rel := range allowed {
		if !found[rel] {
			t.Errorf("allowlisted validate_certs:false site %s no longer disables cert verification; drop it from the allowlist so the guard stays honest", rel)
		}
	}
}

type getURLTaskRef struct {
	rel  string
	name string
	body map[string]any
}

func collectGetURLTasks(rel string, tasks []map[string]any, out *[]getURLTaskRef) {
	for _, task := range tasks {
		for _, key := range []string{"ansible.builtin.get_url", "get_url"} {
			if body, ok := task[key].(map[string]any); ok {
				name, _ := task["name"].(string)
				*out = append(*out, getURLTaskRef{rel: rel, name: name, body: body})
			}
		}
		for _, key := range []string{"block", "rescue", "always"} {
			raw, ok := task[key].([]any)
			if !ok {
				continue
			}
			children := make([]map[string]any, 0, len(raw))
			for _, item := range raw {
				if child, ok := item.(map[string]any); ok {
					children = append(children, child)
				}
			}
			collectGetURLTasks(rel, children, out)
		}
	}
}

func TestGetURLDownloadsPinChecksums(t *testing.T) {
	acceptedUnpinnable := map[string]string{
		bootwrightCollectionRoleRoot + "/controller_openshift_tools/tasks/main.yml\x00Download OpenShift CLI checksums":                         "this IS the sha256sum.txt manifest the sibling tarball fetches pin against; pinning it would be circular",
		bootwrightCollectionRoleRoot + "/controller_virtctl/tasks/main.yml\x00Download virtctl archive":                                         "version-matched virtctl fetched from the live host-cluster ConsoleCLIDownload; the URL and bytes are cluster-derived at run time with no published digest",
		bootwrightCollectionRoleRoot + "/storage_cluster_cephadm/tasks/providers/subscription.yml\x00Install vendor Ceph repository definition": "mutable vendor-published .repo metadata file, not an executed artifact",
	}

	var gets []getURLTaskRef
	walkAnsibleTaskFiles(t, func(rel string) {
		collectGetURLTasks(rel, readAnsibleTasks(t, rel), &gets)
	})
	if len(gets) == 0 {
		t.Fatal("found no get_url tasks; the walk or parse is broken")
	}

	seenAccepted := map[string]bool{}
	sawCephadm := false
	for _, g := range gets {
		_, hasChecksum := g.body["checksum"]

		if strings.HasSuffix(g.rel, "providers/community.yml") && strings.Contains(g.name, "cephadm") {
			sawCephadm = true
			if !hasChecksum {
				t.Errorf("the community cephadm get_url %q in %s must pin a checksum: (M9 fix); it fetches then executes the bootstrap binary", g.name, g.rel)
			}
		}

		if hasChecksum {
			continue
		}
		key := g.rel + "\x00" + g.name
		if _, ok := acceptedUnpinnable[key]; ok {
			seenAccepted[key] = true
			continue
		}
		t.Errorf("get_url %q in %s neither pins a checksum: nor is on the accepted-unpinnable list; an unpinned remote fetch installs/executes unverified content", g.name, g.rel)
	}

	if !sawCephadm {
		t.Error("expected a community cephadm get_url task; the M9 anchor is gone")
	}
	for key := range acceptedUnpinnable {
		if !seenAccepted[key] {
			rel, name, _ := strings.Cut(key, "\x00")
			t.Errorf("accepted-unpinnable get_url %q in %s is gone or now pins a checksum; remove it from the allowlist so the list stays minimal", name, rel)
		}
	}
}

func goFuncBody(t *testing.T, src, signature string) string {
	t.Helper()
	idx := strings.Index(src, signature)
	if idx < 0 {
		t.Fatalf("could not find %q in load.go", signature)
	}
	rest := src[idx+len(signature):]
	if next := strings.Index(rest, "\nfunc "); next >= 0 {
		return rest[:next]
	}
	return rest
}

func TestDesiredStateKindsCompleteInLoadGuards(t *testing.T) {
	src := readRepoFile(t, "internal/state/desired/load.go")
	emptyGuard := goFuncBody(t, src, "func loadFiles(files []string) (v1alpha1.State, error) {")
	sortBody := goFuncBody(t, src, "func sortState(state *v1alpha1.State) {")

	stateType := reflect.TypeOf(v1alpha1.State{})
	checked := 0
	for i := 0; i < stateType.NumField(); i++ {
		field := stateType.Field(i)
		if !field.IsExported() || field.Type.Kind() != reflect.Slice {
			continue
		}
		checked++
		ref := regexp.MustCompile(`\bstate\.` + regexp.QuoteMeta(field.Name) + `\b`)
		if !ref.MatchString(emptyGuard) {
			t.Errorf("v1alpha1.State field %q is missing from the \"no Bootwright YAML documents found\" emptiness guard in loadFiles(); a state of only that kind would wrongly read as empty (the StorageNFSExports drift class)", field.Name)
		}
		if !ref.MatchString(sortBody) {
			t.Errorf("v1alpha1.State field %q is missing from sortState(); that kind would load in nondeterministic order", field.Name)
		}
	}
	if checked == 0 {
		t.Fatal("reflected no exported slice fields on v1alpha1.State; the guard is vacuous")
	}
}
