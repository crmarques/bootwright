package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller/privilege"
)

type unverifiableAccount struct{}

func (unverifiableAccount) Resolve(context.Context) (privilege.Account, error) {
	return privilege.Account{}, errors.New("the account database did not answer")
}

// The privilege boundary refuses before dispatch, so a JSON invocation still
// gets one envelope on standard output that names a resolvable way to run.
func TestAnUnverifiableAccountRefusalCarriesItsRemedyInJSON(t *testing.T) {
	classification := cli.ClassifyInvocation([]string{"status", "--output", "json"})
	if !classification.JSON || !classification.RequiresRoot {
		t.Fatalf("classification = %+v, want a JSON invocation that requires root", classification)
	}
	guard := func(int) (func(), error) {
		t.Fatal("an unverified account armed the sudo parent guard")
		return nil, nil
	}
	_, refusal := privilege.Admit(context.Background(), unverifiableAccount{}, guard)
	if refusal == nil {
		t.Fatal("an unverifiable account was admitted")
	}
	var stdout, stderr bytes.Buffer
	if code := classification.Diagnostic(&stdout, &stderr, *refusal, 1); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	decoder := json.NewDecoder(&stdout)
	var envelope struct {
		OK          bool `json:"ok"`
		ExitCode    int  `json:"exitCode"`
		Diagnostics []struct {
			Code, Message, Remediation string
		} `json:"diagnostics"`
	}
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if decoder.More() {
		t.Fatalf("stdout holds more than one envelope: %q", stdout.String())
	}
	if envelope.OK || envelope.ExitCode != 1 || len(envelope.Diagnostics) != 1 {
		t.Fatalf("envelope = %+v", envelope)
	}
	reported := envelope.Diagnostics[0]
	if reported.Code != "runtime.privilege" || !strings.HasPrefix(reported.Message, "invoking account cannot be verified") ||
		!strings.Contains(reported.Remediation, "clean root login (su -, sudo su -, or a root SSH session)") ||
		!strings.Contains(reported.Remediation, "--context") {
		t.Fatalf("diagnostic = %+v", reported)
	}
}
