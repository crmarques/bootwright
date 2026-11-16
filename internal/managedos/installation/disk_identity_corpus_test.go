package installation

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const diskIdentityCorpus = "../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens/disk_identity_trees.json"

type corpusFile struct {
	Text *string `json:"text"`
	Hex  *string `json:"hex"`
}

func (f corpusFile) bytes(t *testing.T) []byte {
	t.Helper()
	if f.Hex == nil {
		return []byte(*f.Text)
	}
	data, err := hex.DecodeString(*f.Hex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type corpusNode struct {
	Case       string                `json:"case"`
	Files      map[string]corpusFile `json:"files"`
	Serial     *string               `json:"serial"`
	Admissible bool                  `json:"admissible"`
	WWID       *string               `json:"wwid"`
	WWN        string                `json:"wwn"`
	Stricter   map[string]string     `json:"stricter"`
}

func (n corpusNode) disk(t *testing.T) map[string][]byte {
	t.Helper()
	disk := map[string][]byte{}
	for name, file := range n.Files {
		disk[name] = file.bytes(t)
	}
	return disk
}

type corpusGrammar struct {
	Serials []struct {
		Value string `json:"value"`
		Valid bool   `json:"valid"`
	} `json:"serials"`
	Sizes []struct {
		Bytes int64 `json:"bytes"`
		Valid bool  `json:"valid"`
	} `json:"sizes"`
	WWNs []struct {
		WWN   string `json:"wwn"`
		Valid bool   `json:"valid"`
	} `json:"wwns"`
}

func loadDiskIdentityCorpus(t *testing.T) ([]corpusNode, corpusGrammar) {
	t.Helper()
	data, err := os.ReadFile(diskIdentityCorpus)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Nodes   []corpusNode  `json:"nodes"`
		Grammar corpusGrammar `json:"grammar"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Nodes) == 0 {
		t.Fatal("the disk identity corpus holds no node")
	}
	return document.Nodes, document.Grammar
}

// The emitted %pre reads every corpus node's serial number as the erase worker
// and discovery do: the serial resolver finds the disk by exactly the corpus
// serial, and a root declared by serial number passes or stops on the same
// verdict the corpus names. A node the corpus gives no serial, or one only the
// stricter %pre refuses, resolves no disk and reports no readable serial.
func TestTheEmittedPreReadsTheCorpusSerialAsEveryReaderDoes(t *testing.T) {
	nodes, _ := loadDiskIdentityCorpus(t)
	declared := []installerNIC{{"eno1", "52:54:00:9a:1b:01", "e1000e", ""}, {"eno2", "52:54:00:9a:1b:02", "e1000e", ""}}
	for _, node := range nodes {
		t.Run(node.Case, func(t *testing.T) {
			_, refused := node.Stricter["bash"]
			readable := node.Serial != nil && !refused
			probe := "ZC1A2B3C"
			if readable {
				probe = *node.Serial
				if got := resolveCorpusSerial(t, node, probe); got != "1 sda" {
					t.Fatalf("the serial resolver found %q for the corpus serial %q", got, probe)
				}
			} else if got := resolveCorpusSerial(t, node, probe); got != "0 " {
				t.Fatalf("the serial resolver found %q on a node the corpus gives no serial", got)
			}
			if got := resolveCorpusSerial(t, node, "NOT-THE-SERIAL"); got != "0 " {
				t.Fatalf("the serial resolver matched another serial: %q", got)
			}
			input := physicalInstallation()
			input.RootSerial = probe
			if !node.Admissible && readable {
				return
			}
			output, err := runTargetProof(t, input, installerHost{nics: declared, root: "/dev/sda", disk: node.disk(t)})
			if readable && err != nil {
				t.Fatalf("the declared serial %q was refused: %v\n%s", probe, err, output)
			}
			if !readable && (err == nil || !strings.Contains(output, "reports no readable serial number")) {
				t.Fatalf("an unreadable serial was not refused as unreadable: %v\n%s", err, output)
			}
		})
	}
}

func resolveCorpusSerial(t *testing.T, node corpusNode, serial string) string {
	t.Helper()
	blocks := filepath.Join(t.TempDir(), "block")
	for name, file := range node.Files {
		path := filepath.Join(blocks, "sda", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, file.bytes(t), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(blocks, "sda"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(strings.Join(append(identityFunctionLines(), serialResolverLines()...), "\n"), "/sys/class/block", blocks) + "\nbootwright_resolve \"$1\"\n"
	output, err := exec.Command("/bin/bash", "-c", script, "bash", serial).CombinedOutput()
	if err != nil {
		t.Fatalf("the serial resolver failed: %v\n%s", err, output)
	}
	return strings.TrimSuffix(string(output), "\n")
}

// The emitted %pre compares the WWN a node reports exactly as the corpus says:
// a declared WWN passes only on the node whose wwid is its naa form, a node
// reporting nothing readable stops as unreadable, and any other reading is a
// mismatch.
func TestTheEmittedPreReadsTheCorpusWWNAsEveryReaderDoes(t *testing.T) {
	nodes, _ := loadDiskIdentityCorpus(t)
	declared := []installerNIC{{"eno1", "52:54:00:9a:1b:01", "e1000e", ""}, {"eno2", "52:54:00:9a:1b:02", "e1000e", ""}}
	for _, node := range nodes {
		if node.Serial != nil {
			continue
		}
		t.Run(node.Case, func(t *testing.T) {
			wwn := node.WWN
			if wwn == "" {
				wwn = "0x5000c500a1b2c3d4"
			}
			input := physicalInstallation()
			input.RootWWN, input.RootDevice = wwn, "/dev/disk/by-id/wwn-"+wwn
			output, err := runTargetProof(t, input, installerHost{nics: declared, root: "/dev/sda", disk: node.disk(t)})
			switch {
			case node.WWN != "":
				if err != nil {
					t.Fatalf("the corpus WWN %s was refused: %v\n%s", wwn, err, output)
				}
			case node.WWID == nil:
				if err == nil || !strings.Contains(output, "reports no readable WWN") {
					t.Fatalf("an unreadable WWN was not refused as unreadable: %v\n%s", err, output)
				}
			default:
				if err == nil || !strings.Contains(output, "reports a WWN other than "+wwn) {
					t.Fatalf("a WWN %q was not refused as a mismatch: %v\n%s", *node.WWID, err, output)
				}
			}
		})
	}
}

// The renderer admits exactly the corpus serials, sizes and WWNs, so it never
// freezes an identity the corpus says no reader can carry.
func TestTheRendererAdmitsTheCorpusIdentityGrammar(t *testing.T) {
	_, grammar := loadDiskIdentityCorpus(t)
	rendered := func(change func(*Installation)) bool {
		input := physicalInstallation()
		change(&input)
		_, err := RenderKickstart(input)
		return err == nil
	}
	for _, entry := range grammar.Serials {
		if entry.Value == "" {
			continue
		}
		if got := rendered(func(i *Installation) { i.RootSerial = entry.Value }); got != entry.Valid {
			t.Errorf("serial %q: rendered %v, corpus says %v", entry.Value, got, entry.Valid)
		}
	}
	for _, entry := range grammar.Sizes {
		if entry.Bytes == 0 {
			continue
		}
		if got := rendered(func(i *Installation) { i.RootSerial, i.RootSizeBytes = "ZC1A2B3C", entry.Bytes }); got != entry.Valid {
			t.Errorf("size %d: rendered %v, corpus says %v", entry.Bytes, got, entry.Valid)
		}
	}
}
