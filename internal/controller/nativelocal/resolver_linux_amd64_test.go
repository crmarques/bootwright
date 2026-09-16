//go:build linux && amd64

package nativelocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/bundlelocal"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func TestRepositoryMetadataRequiresExactBoundedPublisherMembers(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(fmt.Sprint(corrupt), func(t *testing.T) {
			content := []byte("qualified compressed repository metadata")
			digest := sha256.Sum256(content)
			checksum := hex.EncodeToString(digest[:])
			manifest := fmt.Sprintf(`<repomd><data type="primary"><checksum type="sha256">%s</checksum><location href="repodata/primary.xml.zst"/><size>%d</size></data><data type="filelists"><checksum type="sha256">%s</checksum><location href="repodata/filelists.xml.zst"/><size>%d</size></data></repomd>`, checksum, len(content), checksum, len(content))
			resolver := &Resolver{metadata: func(_ context.Context, method, endpoint string, limit int64, _ prerequisites.SetupEgress) ([]byte, int64, error) {
				if method != http.MethodGet || !strings.HasPrefix(endpoint, "https://publisher.example.test/os/repodata/") {
					t.Fatal(method, endpoint)
				}
				if strings.HasSuffix(endpoint, "repomd.xml") {
					return []byte(manifest), int64(len(manifest)), nil
				}
				if limit != int64(len(content)) {
					t.Fatal("metadata bound not frozen")
				}
				if corrupt {
					return []byte("replacement"), 11, nil
				}
				return content, int64(len(content)), nil
			}}
			repo := repository{ID: "test", BaseURL: "https://publisher.example.test/os"}
			directory := t.TempDir()
			err := resolver.stageRepository(t.Context(), directory, &repo, prerequisites.SetupEgress{})
			if corrupt {
				if err == nil {
					t.Fatal("changed metadata accepted")
				}
				return
			}
			if err != nil || repo.MetadataSHA256 == "" {
				t.Fatal(repo, err)
			}
			data, err := os.ReadFile(filepath.Join(repo.LocalPath, "repodata", "primary.xml.zst"))
			if err != nil || string(data) != string(content) {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeCurrentOSResolution(t *testing.T) {
	if os.Getenv("BOOTWRIGHT_NATIVE_RESOLVE_QUALIFY") != "1" {
		t.Skip("opt-in current-OS public metadata qualification")
	}
	release, err := os.ReadFile("/etc/os-release")
	if err != nil || !strings.Contains(string(release), "ID=fedora\n") || !strings.Contains(string(release), "VERSION_ID=43\n") {
		t.Skip("qualification is only applicable to the current Fedora 43 OS")
	}
	plan, err := New(bundlelocal.FetchMetadata).Resolve(t.Context(), prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}, prerequisites.NativeRequirements{ContainerRuntime: true}, controller.DefaultDependencyVersions(), prerequisites.SetupEgress{NoProxy: []string{}})
	if err != nil {
		detail, _ := json.Marshal(err)
		t.Fatal(string(detail))
	}
	if prerequisites.ValidateNativePlan(plan) != nil || len(plan.Roots) != 3 || len(plan.Packages) == 0 || len(plan.Repositories) != 2 {
		t.Fatal("invalid actual native solver evidence")
	}
	if output := os.Getenv("BOOTWRIGHT_NATIVE_RESOLVE_PLAN"); output != "" {
		data, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(data); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("resolved %d required roots, %d package sources, %d native changes", len(plan.Roots), len(plan.Packages), len(plan.Actions))
}

func TestNativeCurrentOSInspection(t *testing.T) {
	input := os.Getenv("BOOTWRIGHT_NATIVE_INSPECT_PLAN")
	if input == "" {
		t.Skip("opt-in current-OS frozen native file verification")
	}
	data, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	var plan prerequisites.NativeResolvedPlan
	if json.Unmarshal(data, &plan) != nil || prerequisites.ValidateNativePlan(plan) != nil {
		t.Fatal("invalid qualification plan")
	}
	presence, err := New(nil).Check(t.Context(), plan)
	if err != nil || !presence.Ready {
		t.Fatalf("frozen native root presence ready=%t: %v", presence.Ready, err)
	}
	t.Logf("found %d selected root packages installed by name", len(presence.Installed))
}

// Presence evidence must name every selected root in plan order; installed
// identities are display data and are bounded, never compared to the plan.
func TestPresenceEvidenceNamesEverySelectedRoot(t *testing.T) {
	plan := prerequisites.NativeResolvedPlan{Roots: []prerequisites.NativeRoot{
		{Key: "podman", Package: prerequisites.NativeIdentity{Name: "podman", Version: "5.8.4", Release: "1.fc43", Architecture: "x86_64"}},
		{Key: "nmstate", Package: prerequisites.NativeIdentity{Name: "nmstate", Version: "2.2.0", Release: "1.fc43", Architecture: "x86_64"}},
	}}
	newer := `{"key":"podman","name":"podman","installed":{"name":"podman","epoch":0,"version":"5.8.5","release":"1.fc43","architecture":"x86_64"}}`
	nmstate := `{"key":"nmstate","name":"nmstate","installed":{"name":"nmstate","epoch":0,"version":"2.2.0","release":"1.fc43","architecture":"x86_64"}}`
	presence, err := decodePresence(plan, []byte(`{"roots":[`+newer+`,`+nmstate+`],"rootsReady":true}`))
	if err != nil || !presence.Ready || len(presence.Installed) != 2 || presence.Installed[0].Package.Version != "5.8.5" {
		t.Fatalf("presence = %+v %v", presence, err)
	}
	presence, err = decodePresence(plan, []byte(`{"roots":[`+newer+`,{"key":"nmstate","name":"nmstate","installed":null}],"rootsReady":false}`))
	if err != nil || presence.Ready || len(presence.Installed) != 1 {
		t.Fatalf("missing root presence = %+v %v", presence, err)
	}
	for name, data := range map[string]string{
		"omitted-root":     `{"roots":[` + newer + `],"rootsReady":true}`,
		"reordered-root":   `{"roots":[` + nmstate + `,` + newer + `],"rootsReady":true}`,
		"renamed-root":     `{"roots":[` + strings.Replace(newer, `"name":"podman","installed"`, `"name":"docker","installed"`, 1) + `,` + nmstate + `],"rootsReady":true}`,
		"ready-without":    `{"roots":[` + newer + `,{"key":"nmstate","name":"nmstate","installed":null}],"rootsReady":true}`,
		"unbounded":        `{"roots":[` + strings.Replace(newer, `"release":"1.fc43"`, `"release":"`+strings.Repeat("r", 129)+`"`, 1) + `,` + nmstate + `],"rootsReady":true}`,
		"unknown-evidence": `{"roots":[` + newer + `,` + nmstate + `],"rootsReady":true,"inventory":[]}`,
	} {
		if _, err := decodePresence(plan, []byte(data)); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}
