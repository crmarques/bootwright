package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestFlagRelationshipsAndFormats(t *testing.T) {
	cases := []struct {
		args  string
		valid bool
	}{
		{"context init --name demo -f input", true}, {"context init --name demo -f input -f other", false}, {"context init --name Demo -f input", false}, {"context delete --name demo --purge=false", false}, {"context delete --name demo --purge --yes=false", true},
		{"validate -fone,two -fthree", true}, {"apply --authorize=data-loss", true}, {"destroy --authorize=all", false}, {"apply --authorize=data-loss,data-loss", false}, {"apply --authorize=data-loss --authorize=data-loss", false}, {"apply --authorize=data-loss,", false},
		{"add-ons add --name demo:1.0", true}, {"add-ons add --name demo:1.0 --version=", true}, {"add-ons add --name demo:1.0 --version=2.0", false}, {"add-ons delete --name demo:", false}, {"add-ons add --name https://example.invalid/package", false}, {"add-ons add --name ./package", false}, {"add-ons add --name demo --version ../release", false},
		{"secret set --name demo", false}, {"secret set --name demo --value-file source", true}, {"secret set --name demo --value-file source --yes=false", true}, {"secret set --name demo --value-file source --password-stdin=false", true}, {"secret set --name demo --value-stdin", true}, {"secret set --name demo --value-file source --value-stdin", false}, {"secret set --name demo --password-file password --username account", true}, {"secret set --name demo --password-stdin --username account", true}, {"secret set --name demo --password-file password", false}, {"secret set --name demo --username account", false}, {"secret set --name demo --certificate-file cert", true}, {"secret set --name demo --certificate-file cert --private-key-file key", true}, {"secret set --name demo --private-key-file key", true}, {"secret set --name demo --private-key-file key --public-key-file key.pub", true}, {"secret set --name demo --public-key-file key.pub", false}, {"secret set --name demo --value-file source --certificate-file cert", false}, {"secret set --name demo --certificate-file cert --private-key-file key --public-key-file key.pub", false},
		{"secret show --name demo", false}, {"secret show --name demo --part value", true}, {"secret show --name demo --part username", true}, {"secret show --name demo --part password", true}, {"secret show --name demo --part certificate", true}, {"secret show --name demo --part private-key", true}, {"secret show --name demo --part public-key", true}, {"secret show --name demo --part primary", false},
		{"secret encryption init", true}, {"secret encryption init --type local-keyring", false}, {"secret encryption init --type future-store", false}, {"secret encryption init --type LOCAL", false}, {"secret encryption init --type local/keyring", false}, {"secret encryption init --type -local", false},
		{"media add --name image.iso --from-file image.iso --sha256=", true}, {"media add --name image.iso --from-url https://example.invalid/image.iso", false}, {"media add --name image.iso --from-file image.iso --from-url https://example.invalid/image.iso", false}, {"media add --name image.iso --from-file image.iso --sha256 invalid", false},
		{"status --watch-interval=invalid", false}, {"status --watch-interval=-5s", true}, {"status --watch --watch-interval=0s", true}, {"status --watch --output=json", false}, {"status --watch=false --output=json", true},
		{"render --input-dir input", false}, {"render --input-dir input --output-dir output", true}, {"render --input-dir input --output-dir output --sensitive", false}, {"render --input-dir input --output-dir output --context demo", false}, {"render --input-dir input --output-dir output --context=", true}, {"render --output-dir output", false}, {"render --output-dir output --sensitive", true},
		{"machine trust --output json", false}, {"machine trust --output json --dry-run", true}, {"machine trust --output json --yes", true}, {"machine trust --machines one,two --replace two", true}, {"machine trust --machines one --replace two", false},
		{"cluster info --secrets", false}, {"cluster info --name demo --secrets", true}, {"cluster info --secrets=false", true},
		{"preflight infra --clusters=", true}, {"preflight infra --clusters=,", false}, {"render storage --clusters=,", false}, {"machine list --clusters=,", true}, {"machine trust --machines=, --replace=,", true},
		{"version --context=", true}, {"version --context=bad.name", false}, {"version --ssh-user=account", true}, {"version --ssh-user=Account", false}, {"version --ssh-user-for-provisioned", false}, {"version --ssh-user=account --ssh-user-for-provisioned", true}, {"status --ssh-ask-sudo-password --output=json", false}, {"status --ssh-ask-sudo-password=false --output=json", true},
		{"context use --name= --name=demo", false}, {"context use --name=INVALID --name=demo", true}, {"status --output=invalid --output=text", true}, {"status --output= --output=text", false}, {"status --watch-interval=invalid --watch-interval=5s", true}, {"version --ssh-user= --ssh-user=account", false}, {"version --context= --context=demo", true},
	}
	for _, tc := range cases {
		t.Run(tc.args, func(t *testing.T) {
			code, out, errOut, record := runRecorded(strings.Fields(tc.args))
			if tc.valid {
				if code == 2 {
					t.Fatalf("valid input rejected: out=%q err=%q", out, errOut)
				}
			} else if code != 2 || record.calls != 0 {
				t.Fatalf("invalid input code=%d calls=%d out=%q err=%q", code, record.calls, out, errOut)
			}
		})
	}
}

func parsedCommand(t *testing.T, args []string) *cobra.Command {
	t.Helper()
	root, err := newCommandTree(New(Config{}))
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := resolveInvocation(root, args)
	if err != nil {
		t.Fatal(err)
	}
	if err := invocation.command.ParseFlags(invocation.arguments); err != nil {
		t.Fatal(err)
	}
	path := strings.TrimPrefix(invocation.command.CommandPath(), "bootwright ")
	if message := validateInvocation(invocation.command, path); message != "" {
		t.Fatal(message)
	}
	return invocation.command
}

func TestResolvedValuesAndDefaults(t *testing.T) {
	command := parsedCommand(t, []string{"preflight", "infra", "--clusters", " one, two,one, ", "--ssh-user", " account "})
	if stringValue(command.Flags(), "clusters") != "one,two" || stringValue(command.Flags(), "ssh-user") != "account" || !boolValue(command.Flags(), "trust-on-first-use") || stringValue(command.Flags(), "output") != "text" {
		t.Fatal("preflight normalized values or defaults")
	}
	command = parsedCommand(t, []string{"machine", "list", "--clusters", "one", "--clusters", ",,"})
	if stringValue(command.Flags(), "clusters") != "," {
		t.Fatal("explicit empty machine selection was lost")
	}
	command = parsedCommand(t, []string{"machine", "list", "--clusters", "  "})
	if stringValue(command.Flags(), "clusters") != "" {
		t.Fatal("blank selection should mean all")
	}
	command = parsedCommand(t, []string{"status", "--watch", "--watch-interval=-1s"})
	if stringValue(command.Flags(), "watch-interval") != "5s" {
		t.Fatal("non-positive watch interval default")
	}
	command = parsedCommand(t, []string{"status", "--watch-interval=-1s"})
	if stringValue(command.Flags(), "watch-interval") != "-1s" {
		t.Fatal("unused valid interval was changed")
	}
	command = parsedCommand(t, []string{"media", "add", "--name", "image.iso", "--from-url", "https://example.invalid/image.iso", "--sha256", "sha256:" + strings.Repeat("AB", 32)})
	if stringValue(command.Flags(), "sha256") != strings.Repeat("ab", 32) {
		t.Fatal("digest normalization")
	}
	command = parsedCommand(t, []string{"validate", "-fone,two", "-f", "three"})
	if got := arrayValue(command.Flags(), "file"); len(got) != 2 || got[0] != "one,two" || got[1] != "three" {
		t.Fatalf("file arguments %q", got)
	}
}

func TestMediaNameAndURLBoundaries(t *testing.T) {
	for _, value := range []string{"a.iso", "Image_1.2-test.iso", strings.Repeat("a", 251) + ".iso"} {
		if !mediaName(value) {
			t.Errorf("valid basename rejected: %q", value)
		}
	}
	for _, value := range []string{".iso", "a.ISO", "../image.iso", "a/b.iso", "a\\b.iso", "a_.iso", "_a.iso", "CON.iso", "prn.iso", "AUX.iso", "nul.iso", "com9.iso", "LPT1.iso", "á.iso", strings.Repeat("a", 252) + ".iso"} {
		if mediaName(value) {
			t.Errorf("unsafe basename accepted: %q", value)
		}
	}
	for _, value := range []string{"https://account@example.invalid/image.iso", "file:///image.iso", "https:///image.iso", "https://[invalid/image.iso"} {
		args := []string{"media", "add", "--name", "image.iso", "--from-url", value, "--sha256", strings.Repeat("a", 64)}
		code, _, _, record := runRecorded(args)
		if code != 2 || record.calls != 0 {
			t.Fatalf("unsafe URL dispatched: %q", value)
		}
	}
}

func TestBooleanSpellings(t *testing.T) {
	for _, value := range []string{"1", "t", "T", "TRUE", "true", "True", "0", "f", "F", "FALSE", "false", "False"} {
		code, _, errOut, record := runRecorded([]string{"apply", "-v=" + value})
		if code != 1 || record.calls != 1 {
			t.Errorf("valid bool %q rejected: %s", value, errOut)
		}
	}
	for _, value := range []string{"yes", "no", "TrUe", ""} {
		code, _, _, record := runRecorded([]string{"apply", "--verbose=" + value, "--help"})
		if code != 2 || record.calls != 0 {
			t.Errorf("invalid bool %q bypassed syntax", value)
		}
	}
}

func TestStageSelectionIsAClosedCommaList(t *testing.T) {
	command := parsedCommand(t, []string{"apply", "--stage", " clusters , infra-components ,, clusters "})
	if got := stringValue(command.Flags(), "stage"); got != "clusters,infra-components" {
		t.Fatalf("normalized stages = %q", got)
	}
	command = parsedCommand(t, []string{"plan", "--stage", "machines"})
	if got := stringValue(command.Flags(), "stage"); got != "machines" {
		t.Fatalf("plan stages = %q", got)
	}
	for _, args := range [][]string{
		{"apply", "--stage", "storage"},
		{"apply", "--stage", ",,"},
		{"apply", "--stage="},
		{"plan", "--stage", "Machines"},
		{"destroy", "--stage", "machines"},
	} {
		code, _, _, record := runRecorded(args)
		if code != 2 || record.calls != 0 {
			t.Errorf("invalid stage selection dispatched: %v", args)
		}
	}
}

// An omitted selection means every stage, so it must not be rejected as an
// unsupported enum value the way a scalar enum flag would be.
func TestOmittedStageSelectionDispatches(t *testing.T) {
	code, _, _, record := runRecorded([]string{"apply"})
	if code != 1 || record.calls != 1 {
		t.Fatalf("an omitted stage selection did not dispatch: code=%d calls=%d", code, record.calls)
	}
}
