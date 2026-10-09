//go:build linux && amd64

package nativelocal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// The runner reads synthetic evidence only from the database directory the
// production command selects. Neither a host rpm nor a live database is used.
func TestRPMQueryReadsThePlatformSnapshot(t *testing.T) {
	for _, test := range []struct {
		name, os, release, directory, format, key string
		limit                                     int
	}{
		{"Fedora foundation", "fedora", "43", "usr/lib/sysimage/rpm", foundationQueryFormat, fedoraKeyID, foundationQueryLimit},
		{"RHEL foundation", "rhel", "9.8", "var/lib/rpm", foundationQueryFormat, vendorKeyID, foundationQueryLimit},
		{"RHEL operator", "rhel", "9.8", "var/lib/rpm", rpmQueryFormat, vendorKeyID, 64 << 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			platform := prerequisites.Platform{OS: test.os, Release: test.release, Architecture: "amd64"}
			snapshot := filepath.Join(t.TempDir(), "snapshot")
			directory := filepath.Join(snapshot, test.directory)
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			name := "libgcc"
			if test.format == rpmQueryFormat {
				name = "xorriso"
			}
			evidence := queryLine(name, "1", signature(test.key), "(none)")
			if test.format == foundationQueryFormat {
				evidence = strings.TrimSuffix(evidence, "\n") + "\t8\n/lib64/libgcc.so.1\t" + strings.Repeat("a", 64) + "\t\n"
			}
			file := filepath.Join(directory, "rpmdb.sqlite")
			if err := os.WriteFile(file, []byte(evidence), 0600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			run := func(command *exec.Cmd) error {
				calls++
				want := []string{"/usr/bin/rpm", "--dbpath", directory, "-q", "--queryformat", test.format, "--", name}
				if command.Path != "/usr/bin/rpm" || !slices.Equal(command.Args, want) {
					t.Fatalf("rpm command = %q; want %q", command.Args, want)
				}
				if command.Dir != "/" || !slices.Equal(command.Env, helperEnvironment(filepath.Join(filepath.Dir(snapshot), "home"))) || command.WaitDelay != 2*time.Second || command.Cancel == nil || command.SysProcAttr == nil || !command.SysProcAttr.Setpgid {
					t.Fatal("rpm lost its isolated execution settings")
				}
				credential := command.SysProcAttr.Credential
				if os.Geteuid() == 0 && (credential == nil || credential.Uid != unprivilegedID || credential.Gid != unprivilegedID || credential.NoSetGroups) {
					t.Fatal("rpm lost its unprivileged identity")
				}
				if command.Stdout.(*boundedBuffer).limit != test.limit || command.Stderr.(*boundedBuffer).limit != 64<<10 {
					t.Fatal("rpm lost its output bounds")
				}
				data, err := os.ReadFile(filepath.Join(command.Args[2], "rpmdb.sqlite"))
				if err != nil {
					return err
				}
				_, err = command.Stdout.Write(data)
				return err
			}
			result, err := runRPMQuery(context.Background(), platform, snapshot, name, test.format, test.limit, run)
			if err != nil || string(result.Stdout) != evidence || result.Exit != 0 || len(result.Stderr) != 0 || calls != 1 {
				t.Fatalf("snapshot evidence = %+v, calls = %d (%v)", result, calls, err)
			}
			if test.format == foundationQueryFormat {
				builds, err := parseFoundationQuery(name, result, test.key)
				if err != nil || len(builds) != 1 || !builds[0].Signed || builds[0].Files["/lib64/libgcc.so.1"].SHA256 != strings.Repeat("a", 64) {
					t.Fatalf("foundation evidence = %+v (%v)", builds, err)
				}
			} else if presence, err := operatorPresence([]string{name}, map[string]operatorQuery{name: result}, test.key); err != nil || !presence.Ready {
				t.Fatalf("operator evidence = %+v (%v)", presence, err)
			}

			// A lost snapshot refuses with one attempt at the same private path;
			// it must never retry against rpm's default live database.
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			calls = 0
			if _, err := runRPMQuery(context.Background(), platform, snapshot, name, test.format, test.limit, run); err == nil || calls != 1 {
				t.Fatalf("missing snapshot: calls = %d (%v)", calls, err)
			}
		})
	}
}

func TestRPMQueryRefusesUnsupportedPlatformBeforeRunning(t *testing.T) {
	calls := 0
	_, err := runRPMQuery(context.Background(), prerequisites.Platform{OS: "unsupported"}, t.TempDir(), "libgcc", foundationQueryFormat, foundationQueryLimit, func(*exec.Cmd) error {
		calls++
		return nil
	})
	if err == nil || calls != 0 {
		t.Fatalf("unsupported platform: calls = %d (%v)", calls, err)
	}
}
