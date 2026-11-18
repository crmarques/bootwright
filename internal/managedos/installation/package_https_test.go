package installation

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func httpsTreeProfile(profile api.Object) api.Object {
	return profile.WithSpec(profile.Spec().WithPath(api.StringValue("ip-https"),
		"installer", "anaconda", "packageSource", "hostedTree", "artifactServerEndpoint", "endpointRef"))
}

func TestHTTPSPackageTreeFreezesItsOwnTrustBeforeTheFirstFetch(t *testing.T) {
	for _, profile := range []api.Object{installProfile(), encryptingProfile()} {
		request, _ := onlyRequest(t, labCatalog(httpsTreeProfile(profile)))
		if request.Tree.CertificateRef != "lab-artifacts-tls" {
			t.Fatal("the HTTPS tree lost its serving certificate binding")
		}
		const ca = "/tmp/bootwright-package-tree/ca.pem"
		requireLine(t, request.Kickstart, "url --url="+request.Tree.URL+" --sslcacert="+ca)
		if !strings.Contains(request.Kickstart, "@@BOOTWRIGHT_PACKAGE_TREE_CERTIFICATE@@") {
			t.Fatal("the installer has no operation-bound package tree trust before payload setup")
		}
		for _, forbidden := range []string{"--noverifyssl", "sslverify=false", "update-ca-trust", "treeIdentity", "treePrivateKey"} {
			if strings.Contains(request.Kickstart, forbidden) {
				t.Fatal("package trust contains an unrelated or unverified mechanism: " + forbidden)
			}
		}
		staged := strings.Index(request.Kickstart, "cat > "+ca)
		if staged < 0 || (request.DiskEncryption != nil && staged >= strings.Index(request.Kickstart, "bootwright_recovery=$(curl ")) {
			t.Fatal("package trust must be staged before the recovery fetch")
		}
	}
}

func TestHTTPSPackageTreeRefusesAMissingTrustBinding(t *testing.T) {
	_, err := packageSource(Request{Tree: &Publication{URL: "https://artifacts.example.test:7443/tree"}})
	if err == nil || diagnostics.Of(err)[0].Code != "install.input-invalid" {
		t.Fatal("an HTTPS tree without its bound serving certificate must refuse")
	}
}

func TestHTTPAndLocalPackageSourcesDoNotStageHTTPSMaterial(t *testing.T) {
	request, _ := onlyRequest(t, labCatalog())
	if !strings.Contains(request.Kickstart, "url --url=http://") || strings.Contains(request.Kickstart, "--sslcacert") || strings.Contains(request.Kickstart, "@@BOOTWRIGHT_PACKAGE_TREE_CERTIFICATE@@") {
		t.Fatal("HTTP source semantics changed")
	}
	source, err := packageSource(Request{})
	if err != nil || source != "cdrom" {
		t.Fatal("local DVD source semantics changed")
	}
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := DecodeRequest(canonical)
	if err != nil || saved.Kickstart != request.Kickstart {
		t.Fatal("a frozen HTTP Kickstart was changed during execution decoding")
	}
}

func TestPackageTrustRefusesInconsistentBootstrapState(t *testing.T) {
	for _, input := range []Installation{
		{PackageSource: "cdrom", PackageTLS: true},
		{PackageSource: "url --url=https://artifacts.example.test/tree"},
		{PackageSource: "url --url=https://artifacts.example.test/tree --sslcacert=" + packageTrustPath},
	} {
		if err := packageTrustRefusal(input); err == nil {
			t.Fatal("inconsistent package-source trust was accepted")
		}
	}
}

func TestPackageTrustStageCreatesPrivateFilesAndRefusesExistingPaths(t *testing.T) {
	for _, existing := range []string{"absent", "directory", "symlink"} {
		t.Run(existing, func(t *testing.T) {
			directory := t.TempDir()
			outside := filepath.Join(directory, "outside")
			if err := os.WriteFile(outside, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			trust := filepath.Join(directory, "trust")
			if existing == "directory" {
				if err := os.Mkdir(trust, 0777); err != nil {
					t.Fatal(err)
				}
			}
			if existing == "symlink" {
				if err := os.Symlink(outside, trust); err != nil {
					t.Fatal(err)
				}
			}
			lines := packageTrustLines(Installation{PackageTLS: true})
			const pem = "-----BEGIN CERTIFICATE-----\nRVhBTVBMRQ==\n-----END CERTIFICATE-----\n"
			script := strings.ReplaceAll(strings.Join(lines[1:len(lines)-2], "\n"), packageTrustDirectory, "trust")
			script = strings.ReplaceAll(script, PackageCertificateToken, pem)
			command := exec.Command("/bin/bash", "-c", script)
			command.Dir = directory
			output, err := command.CombinedOutput()
			if (err == nil) != (existing == "absent") {
				t.Fatalf("stage %s: %v: %s", existing, err, output)
			}
			original, err := os.ReadFile(outside)
			if err != nil || string(original) != "untouched" {
				t.Fatal("existing path was modified")
			}
			if existing != "absent" {
				return
			}
			body, err := os.ReadFile(filepath.Join(trust, "ca.pem"))
			if err != nil || string(body) != pem+"\n" {
				t.Fatalf("staged bytes: %q (%v)", body, err)
			}
			for path, mode := range map[string]os.FileMode{trust: 0700, filepath.Join(trust, "ca.pem"): 0600} {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != mode {
					t.Fatal("trust file permissions are not private")
				}
			}
		})
	}
}
