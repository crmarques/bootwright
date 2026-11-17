package v1alpha1

import (
	"maps"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// lexicalSeeds is each lexical rule with one value it admits. The fuzz target
// takes a rule by its place in this table, and the test below holds the table
// to the rules ValidLexical names.
var lexicalSeeds = map[LexicalRule]string{
	"kubernetes-label-key":   "example.test/app-name",
	"kubernetes-label-value": "value_1",
	"name":                   "lab-one",
	"dns":                    "host.example.test",
	"host":                   "192.0.2.10",
	"ip":                     "2001:db8::1",
	"cidr":                   "192.0.2.0/24",
	"http-url":               "http://repo.example.test:8080/path",
	"https-url":              "https://repo.example.test/path",
	"repository-url":         "https://repo.example.test/os/$basearch",
	"mirror-url":             "https://mirror.example.test/base",
	"artifact-mirror-url":    "https://mirror.example.test/artifacts",
	"proxy-endpoint":         "http://proxy.example.test:3128",
	"proxy-bypass":           "*.example.test:443",
	"proxy-destination":      "[2001:db8::1]:443",
	"systemd-unit":           "chronyd.service",
	"package-spec":           "@module:stream/profile",
	"kickstart-token":        "en_US.UTF-8",
	"ifname":                 "enp1s0",
	"ceph-host-label":        "_admin",
	"bridge":                 "br-lab",
	"registry":               "registry.example.test:5000/mirror",
	"registry-base":          "registry.example.test",
	"image":                  "registry.example.test/team/app:1.2",
	"package-repository":     "https://repo.example.test/el9",
	"package-repository-key": "https://repo.example.test/RPM-GPG-KEY",
	"native-version":         "1.2.3-rc.1+build",
	"image-version":          "v1.2.3",
	"cluster-release":        "4.20.1",
	"os-release":             "9.6",
	"cli-version":            "v1.2.3",
	"package-version":        "1.2.3-4.el9",
	"duration":               "90s",
	"mac":                    "52:54:00:aa:bb:cc",
	"absolute-path":          "/var/lib/bootwright",
	"device-path":            "/dev/disk/by-id/nvme-1",
	"relative-path":          "assets/one.txt",
	"token":                  "token-1.2_3",
	"posix-user":             "bootwright",
	"checksum":               "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
}

// lexicalBounds is each rule's longest admitted value in bytes, where its
// grammar bounds one. A rule not listed here has none.
var lexicalBounds = map[LexicalRule]int{
	"kubernetes-label-key": 253 + 1 + 63,
	"name":                 63,
	"dns":                  253,
	"host":                 253,
	"ip":                   45,
	"cidr":                 49,
	"mirror-url":           maxMirrorURLBytes,
	"artifact-mirror-url":  maxMirrorURLBytes,
	"proxy-endpoint":       maxProxyEndpointBytes,
	"proxy-bypass":         maxProxyBypassBytes,
	"systemd-unit":         255,
	"ifname":               15,
	"bridge":               15,
	"package-repository":   2048,
	"native-version":       1024,
	"image-version":        128 + len("sha256:"),
	"cluster-release":      96,
	"os-release":           64,
	"cli-version":          96,
	"package-version":      96,
}

func lexicalRuleNames() []LexicalRule {
	return slices.Sorted(maps.Keys(lexicalSeeds))
}

func FuzzValidLexical(f *testing.F) {
	rules := lexicalRuleNames()
	for index, rule := range rules {
		f.Add(uint8(index), lexicalSeeds[rule])
		f.Add(uint8(index), "")
		f.Add(uint8(index), " "+lexicalSeeds[rule])
		f.Add(uint8(index), lexicalSeeds[rule]+"\n")
		f.Add(uint8(index), lexicalSeeds[rule][:len(lexicalSeeds[rule])/2]+"\x00é"+lexicalSeeds[rule][len(lexicalSeeds[rule])/2:])
	}
	f.Fuzz(func(t *testing.T, index uint8, value string) {
		if len(value) > 8192 {
			t.Skip()
		}
		rule := rules[int(index)%len(rules)]
		accepted := ValidLexical(rule, value)
		if ValidLexical(rule, value) != accepted {
			t.Fatalf("%s judged %q twice with different answers", rule, value)
		}
		if !accepted {
			return
		}
		if strings.ContainsRune(value, 0) {
			t.Fatalf("%s admitted %q holding a NUL", rule, value)
		}
		if rule != "checksum" && strings.TrimSpace(value) != value {
			t.Fatalf("%s admitted %q with surrounding white space", rule, value)
		}
		if value == "" && rule != "kubernetes-label-value" {
			t.Fatalf("%s admitted the empty value", rule)
		}
		if limit, bounded := lexicalBounds[rule]; bounded && len(value) > limit {
			t.Fatalf("%s admitted %d bytes, above its %d-byte bound", rule, len(value), limit)
		}
		if rule == "native-version" && !utf8.ValidString(value) {
			t.Fatalf("%s admitted invalid UTF-8 %q", rule, value)
		}
		if !strings.HasSuffix(string(rule), "-path") && rule != "native-version" && rule != "checksum" && rule != "duration" && rule != "mac" {
			for index := range len(value) {
				if value[index] < 0x21 || value[index] > 0x7e {
					t.Fatalf("%s admitted %q holding a byte outside printable ASCII", rule, value)
				}
			}
		}
	})
}

func TestLexicalFuzzTableHoldsEveryRuleValidLexicalNames(t *testing.T) {
	if len(lexicalRules) == 0 {
		t.Fatal("found no rule in the lexical registry")
	}
	for rule := range lexicalRules {
		if _, seeded := lexicalSeeds[rule]; !seeded {
			t.Errorf("the lexical registry names the rule %q that lexicalSeeds lacks", rule)
		}
	}
	for rule, seed := range lexicalSeeds {
		if _, registered := lexicalRules[rule]; !registered {
			t.Errorf("lexicalSeeds holds %q, which the lexical registry does not name", rule)
		}
		if !ValidLexical(rule, seed) {
			t.Errorf("%s refuses its seed %q", rule, seed)
		}
	}
}
