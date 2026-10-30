package installation

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestEncryptedPreActivatesNetworkBeforeFetchingRecovery(t *testing.T) {
	request, _ := onlyRequest(t, labCatalog(encryptingProfile()))
	pre := strings.Split(strings.Split(request.Kickstart, "%pre --erroronfail --interpreter=/bin/bash\n")[1], "%end")[0]
	proof := strings.Index(pre, "if [ ! -c /dev/tpmrm0 ]; then")
	activation := strings.Index(pre, "nmcli --wait 60 connection up uuid ")
	fetch := strings.Index(pre, "bootwright_recovery=$(curl ")
	if activation < proof || fetch < activation {
		t.Fatal("the encrypted pre-script must prove the TPM, activate its network, then fetch recovery")
	}
}

func TestEncryptedKickstartRefusesMalformedBondKeyfileEntries(t *testing.T) {
	for _, option := range []string{"[ipv4]", "bad-key=value", "=value", "miimon=", "miimon=100\n[ipv4]"} {
		t.Run(option, func(t *testing.T) {
			input := physicalInstallation()
			input.DiskEncryption = &KickstartEncryption{}
			input.Interface, input.InterfaceType = "bond0", "bond"
			input.BondPorts = []string{"eno1"}
			input.BondOptions = []string{"mode=active-backup", option}
			if _, err := RenderKickstart(input); err == nil {
				t.Fatal("malformed option reached the rendered keyfile")
			}
		})
	}
}

func TestEncryptionBootstrapProfilesAndActivation(t *testing.T) {
	for _, kind := range []string{"ethernet", "bond", "vlan", "vlan-bond"} {
		t.Run(kind, func(t *testing.T) {
			input := bootstrapInstallation(kind)
			files, commands, _, err := runBootstrapPre(t, input, "")
			if err != nil {
				t.Fatal(err)
			}
			wantCount := map[string]int{"ethernet": 1, "bond": 3, "vlan": 2, "vlan-bond": 4}[kind]
			if len(files) != wantCount || !strings.HasPrefix(commands[0], "--wait 60 connection load ") {
				t.Fatalf("profiles=%d commands=%q", len(files), commands)
			}
			var addressed int
			for _, file := range files {
				name := keyfileValue(file, "interface-name")
				if keyfileValue(file, "id") != name || keyfileValue(file, "uuid") != bootstrapUUID(input.Hostname, name) {
					t.Fatalf("profile loses its Anaconda identity: %s", file)
				}
				if strings.Contains(file, "address1=") {
					addressed++
					if name != input.Interface || !strings.Contains(file, "address1=192.0.2.10/24\ngateway=192.0.2.1\ndns=192.0.2.53;192.0.2.54;\nmay-fail=false") {
						t.Fatalf("wrong layer-three configuration: %s", file)
					}
					if !strings.Contains(file, "[ipv6]\nmethod=auto\nmay-fail=true") {
						t.Fatalf("install interface lost optional automatic IPv6: %s", file)
					}
				} else if slices.Contains(input.BondPorts, name) {
					if strings.Contains(file, "[ipv4]") || strings.Contains(file, "[ipv6]") {
						t.Fatalf("bond port has an IP configuration: %s", file)
					}
				} else if !strings.Contains(file, "[ipv4]\nmethod=disabled") || !strings.Contains(file, "[ipv6]\nmethod=disabled") {
					t.Fatalf("lower interface has IP enabled: %s", file)
				}
				if slices.Contains(input.BondPorts, name) && (keyfileValue(file, "master") != bootstrapUUID(input.Hostname, bootstrapBase(input)) || keyfileValue(file, "slave-type") != "bond") {
					t.Fatalf("port has the wrong controller: %s", file)
				}
				if keyfileValue(file, "type") == "bond" && !strings.Contains(file, "[bond]\nmode=active-backup\narp_ip_target=192.0.2.1,192.0.2.2") {
					t.Fatalf("bond options lost: %s", file)
				}
				if keyfileValue(file, "type") == "vlan" && !strings.Contains(file, "[vlan]\nid=200\nparent="+bootstrapUUID(input.Hostname, input.VLANBase)) {
					t.Fatalf("VLAN identity lost: %s", file)
				}
			}
			if addressed != 1 {
				t.Fatalf("%d profiles have addresses", addressed)
			}
			want := []string{"--wait 60 connection up uuid " + bootstrapUUID(input.Hostname, bootstrapBase(input))}
			if input.InterfaceType == "vlan" {
				want = append(want, "--wait 60 connection up uuid "+bootstrapUUID(input.Hostname, input.Interface))
			}
			if !slices.Equal(commands[1:], want) {
				t.Fatalf("activation=%q want=%q", commands[1:], want)
			}
		})
	}
}

func TestEncryptionBootstrapFailureNeverFetchesRecoveryOrWritesStorage(t *testing.T) {
	for _, failure := range []string{"load", "up-1", "up-2"} {
		t.Run(failure, func(t *testing.T) {
			_, _, fetched, err := runBootstrapPre(t, bootstrapInstallation("vlan-bond"), failure)
			if err == nil || fetched {
				t.Fatalf("failed activation continued: error=%v fetched=%v", err, fetched)
			}
		})
	}
}

func TestEncryptionBootstrapOptionalSettingsAndLiteralValues(t *testing.T) {
	input := bootstrapInstallation("bond")
	input.Gateway, input.Nameservers = "", nil
	input.BondOptions = append(input.BondOptions, "primary=$(>injected)`>injected`")
	files, _, _, err := runBootstrapPre(t, input, "")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(files, "\n")
	if strings.Contains(joined, "gateway=") || strings.Contains(joined, "dns=") || !strings.Contains(joined, input.BondOptions[len(input.BondOptions)-1]) {
		t.Fatalf("optional settings or literal bond value changed: %s", joined)
	}
	input.DiskEncryption = nil
	if lines := encryptionNetworkLines(input); len(lines) != 0 {
		t.Fatalf("plain installation carries a bootstrap helper: %q", lines)
	}
}

func TestEncryptionBootstrapPreservesResolversInTheirAddressFamilies(t *testing.T) {
	for _, servers := range [][]string{{"2001:db8::53"}, {"2001:db8::53", "192.0.2.53"}} {
		input := bootstrapInstallation("ethernet")
		input.Nameservers = servers
		files, _, _, err := runBootstrapPre(t, input, "")
		if err != nil {
			t.Fatal(err)
		}
		ipv4, ipv6, found := strings.Cut(files[0], "[ipv6]")
		if !found || !strings.Contains(ipv6, "dns=2001:db8::53;") || strings.Contains(ipv4, "2001:db8::53") || strings.Contains(ipv4, "dns=192.0.2.53;") != (len(servers) == 2) {
			t.Fatalf("bootstrap resolver changed family or disappeared: %s", files[0])
		}
	}
}

func bootstrapInstallation(kind string) Installation {
	input := Installation{Address: "192.0.2.10", Prefix: 24, Hostname: "node.example.test", Gateway: "192.0.2.1",
		Nameservers: []string{"192.0.2.53", "192.0.2.54"}, Interface: "eno1", InterfaceType: "ethernet", DiskEncryption: &KickstartEncryption{}}
	if kind == "bond" || kind == "vlan-bond" {
		input.Interface, input.InterfaceType = "bond0", "bond"
		input.BondPorts = []string{"eno1", "eno2"}
		input.BondOptions = []string{"mode=active-backup", "arp_ip_target=192.0.2.1,192.0.2.2", "arp_interval=100"}
	}
	if kind == "vlan" || kind == "vlan-bond" {
		input.VLANBase, input.VLANID = input.Interface, 200
		input.Interface, input.InterfaceType = "storage", "vlan"
	}
	return input
}

func bootstrapBase(input Installation) string {
	if input.InterfaceType == "vlan" {
		return input.VLANBase
	}
	return input.Interface
}

func keyfileValue(file, key string) string {
	for _, line := range strings.Split(file, "\n") {
		if value, ok := strings.CutPrefix(line, key+"="); ok {
			return value
		}
	}
	return ""
}

// Execute the actual %pre with an isolated /tmp and profile directory. Only
// the TPM existence check and external nmcli/curl are replaced; set -e,
// heredocs, permissions and the recovery/storage ordering are exercised.
func runBootstrapPre(t *testing.T, input Installation, failure string) ([]string, []string, bool, error) {
	t.Helper()
	root := t.TempDir()
	profiles := filepath.Join(root, "connections")
	// Only tools present in the installer are available. In particular install
	// is absent; omitting optional shred also exercises the existing rm fallback.
	for _, tool := range []string{"mkdir", "chmod", "cat", "rm"} {
		if err := os.Symlink("/bin/"+tool, filepath.Join(root, tool)); err != nil {
			t.Fatal(err)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write("nmcli", `#!/bin/bash
set -eu
printf '%s\n' "$*" >> "$BOOTSTRAP_ROOT/commands"
test "$1 $2 $3" = '--wait 60 connection'
if [ "$4" = load ]; then
  shift 4
  for file; do test -s "$file"; done
  [ "$BOOTSTRAP_FAIL" != load ]
else
  count=0
  if [ -f "$BOOTSTRAP_ROOT/count" ]; then read -r count < "$BOOTSTRAP_ROOT/count"; fi
  count=$((count + 1))
  printf '%s\n' "$count" > "$BOOTSTRAP_ROOT/count"
  [ "$BOOTSTRAP_FAIL" != "up-$count" ]
fi
`)
	write("curl", "#!/bin/bash\n: > \"$BOOTSTRAP_ROOT/fetched\"\nprintf '%s' 'SyntheticRecovery123'\n")
	lines := encryptionPreLines(input)
	script := strings.Join(lines[1:len(lines)-2], "\n")
	script = strings.ReplaceAll(script, "[ ! -c /dev/tpmrm0 ]", "false")
	script = strings.ReplaceAll(script, "/tmp/", root+"/")
	script = strings.ReplaceAll(script, bootstrapConnections, profiles)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/bash", "-c", script)
	command.Dir = root
	command.Env = append(os.Environ(), "PATH="+root, "BOOTSTRAP_ROOT="+root, "BOOTSTRAP_FAIL="+failure)
	output, err := command.CombinedOutput()
	if len(output) != 0 {
		t.Logf("pre-script output: %s", output)
	}
	if _, injected := os.Stat(filepath.Join(root, "injected")); injected == nil {
		t.Fatal("authored value executed as shell code")
	}
	var files []string
	if info, statErr := os.Stat(profiles); statErr != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("profile directory permissions: %v %v", info, statErr)
	}
	entries, readErr := os.ReadDir(profiles)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		info, statErr := entry.Info()
		if statErr != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("profile permissions: %v %v", info, statErr)
		}
		data, readErr := os.ReadFile(filepath.Join(profiles, entry.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		files = append(files, string(data))
	}
	log, readErr := os.ReadFile(filepath.Join(root, "commands"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	_, fetchErr := os.Stat(filepath.Join(root, "fetched"))
	_, storageErr := os.Stat(filepath.Join(root, "bootwright-storage.ks"))
	if (failure == "") != (storageErr == nil) {
		t.Fatalf("storage include presence after %q: %v", failure, storageErr)
	}
	return files, strings.Split(strings.TrimSpace(string(log)), "\n"), fetchErr == nil, err
}
