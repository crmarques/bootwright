package cli

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

func TestCLIVocabulariesMatchOwners(t *testing.T) {
	var stages []string
	for _, stage := range reconciliation.Stages() {
		stages = append(stages, string(stage))
	}
	if !slices.Equal(stageFlag().enum, stages) {
		t.Fatalf("stage enum %v differs from owner %v", stageFlag().enum, stages)
	}
	if !slices.Equal(authorizationFlag().enum, []string{reconciliation.AuthorizationDataLoss}) {
		t.Fatal("authorization vocabulary differs from owner")
	}
	parts := []string{string(secrets.ValuePart), string(secrets.UsernamePart), string(secrets.PasswordPart), string(secrets.CertificatePart), string(secrets.PrivateKeyPart), string(secrets.PublicKeyPart)}
	for _, flag := range specFor("secret show").flags {
		if flag.name == "part" && !slices.Equal(flag.enum, parts) {
			t.Fatalf("secret parts %v differ from owner %v", flag.enum, parts)
		}
	}
}

func TestEveryCatalogPayloadCommandAcceptsOperandsAndSeparator(t *testing.T) {
	for _, spec := range commandCatalog() {
		if !spec.payload {
			continue
		}
		for _, separator := range []bool{false, true} {
			args := append(strings.Fields(spec.path), "--name", "example")
			payload := []string{"echo", "--help"}
			if separator {
				args = append(args, "--")
			}
			args = append(args, payload...)
			if !separator && !spec.stopAtPayload {
				payload = []string{"echo", "operand"}
				args[len(args)-1] = "operand"
			}
			code, _, _, record := runRecorded(args)
			if code == 2 || record.calls != 1 {
				t.Fatalf("%s separator %v failed: code %d calls %d", spec.path, separator, code, record.calls)
			}
			value := reflect.ValueOf(record.request).FieldByName("Command")
			if !value.IsValid() || !reflect.DeepEqual(value.Interface(), payload) {
				t.Fatalf("%s payload = %v; want %v", spec.path, record.request, payload)
			}
		}
	}
}

func TestUsageDistinguishesUnknownAndMisplacedFlags(t *testing.T) {
	for _, test := range []struct {
		args    []string
		message string
	}{
		{[]string{"version", "--not-a-flag"}, "unknown flag"},
		{[]string{"version", "--output", "json"}, "flag is not accepted at this position"},
		{[]string{"--name", "example", "context", "init"}, "flag is not accepted at this position"},
	} {
		code, out, errOut, record := runRecorded(test.args)
		if code != 2 || out != "" || record.calls != 0 || !strings.Contains(errOut, test.message) {
			t.Fatalf("%v: %d %q %q", test.args, code, out, errOut)
		}
	}
}
