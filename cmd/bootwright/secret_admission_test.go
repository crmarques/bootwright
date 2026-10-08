package main

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestProductionAdmissionRefusesADefaultFileSourceAtTheDefaultAndEachRecipient(t *testing.T) {
	const defaultRemedy = "replace source.file in the Environment's Secret kind default with source: {contextStore: {}}, then for each Secret that inherits it run bootwright secret set --name <name> --context <context> with the file flags of its type"
	for _, test := range []struct{ name, fragment, recipientType, remedy string }{
		{"token", "type: token\n      source: {file: {path: ../secrets/material}}", "", defaultRemedy + " (--value-file <path> for token)"},
		{"usernamePassword", "type: usernamePassword\n      source: {file: {path: secrets/credentials}}", "", defaultRemedy + " (--username <username> --password-file <path> for usernamePassword)"},
		{"tlsCertificate", "type: tlsCertificate\n      source: {file: {cert: secrets/tls.crt, key: secrets/tls.key}}", "", defaultRemedy + " (--certificate-file <path> --private-key-file <path> for tlsCertificate)"},
		{"sshKeyPair", "type: sshKeyPair\n      source: {file: {privateKey: secrets/id, publicKey: secrets/id.pub}}", "", defaultRemedy + " (--private-key-file <path> [--public-key-file <path>] for sshKeyPair)"},
		{"untyped", "source: {file: {path: secrets/material}}", "opaque", defaultRemedy},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := serviceEnvironment + "  defaults:\n    Secret:\n      " + test.fragment + "\n"
			recipient := "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata: {name: material}\nspec: {}\n"
			if test.recipientType != "" {
				recipient = strings.Replace(recipient, "spec: {}", "spec: {type: "+test.recipientType+"}", 1)
			}
			state, report, err := wireCompiler().Compile(context.Background(), serviceSources(env, recipient))
			if state != nil || report != nil || err == nil {
				t.Fatalf("a default file source compiled: %v %v %v", state, report, err)
			}
			atDefault, atRecipient := 0, 0
			for _, d := range diagnostics.Of(err) {
				if !strings.HasSuffix(d.Field, "source.file") {
					continue
				}
				if d.Code != "api.field" || strings.Contains(d.Remediation, "--name  ") || strings.Contains(d.Remediation, "--name with") || d.Object == nil {
					t.Fatalf("malformed file source refusal: %#v", d)
				}
				switch {
				case d.Object.Kind == "Environment" && d.Object.Name == "synthetic" && d.Field == "$.spec.defaults.Secret.source.file" && d.Remediation == test.remedy:
					atDefault++
				case d.Object.Kind == "Secret" && d.Object.Name == "material" && d.Field == "$.spec.source.file" && strings.Contains(d.Message, "(from Environment defaults)") && strings.Contains(d.Remediation, "Environment kind default"):
					atRecipient++
				default:
					t.Fatalf("file source refusal names neither the kind default nor its recipient: %#v, want remedy %q", d, test.remedy)
				}
			}
			if atDefault != 1 || atRecipient != 1 {
				t.Fatalf("refusals at default=%d recipient=%d: %#v", atDefault, atRecipient, diagnostics.Of(err))
			}
		})
	}
}

func TestProductionAdmissionFileSourceRemedyNeverPrintsAnInvalidName(t *testing.T) {
	secret := "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata: {name: Material}\nspec:\n  type: token\n  source: {file: {path: secrets/material}}\n"
	state, report, err := wireCompiler().Compile(context.Background(), serviceSources(serviceEnvironment, secret))
	if state != nil || report != nil || err == nil {
		t.Fatalf("a file source compiled: %v %v %v", state, report, err)
	}
	want := "declare source: {contextStore: {}} and run bootwright secret set --name <name> --context <context> --value-file <path>"
	for _, d := range diagnostics.Of(err) {
		if d.Field == "$.spec.source.file" {
			if d.Code != "api.field" || d.Remediation != want {
				t.Fatalf("refusal = %#v, want remedy %q", d, want)
			}
			return
		}
	}
	t.Fatalf("missing file source refusal: %#v", diagnostics.Of(err))
}
