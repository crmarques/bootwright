package sshlocal

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
)

func TestEvaluatedHostKeyAlgorithmsAreParsedStrictly(t *testing.T) {
	dump := "user bootwright\nhostname localhost\nport 22\n" +
		"ciphers aes256-gcm@openssh.com,aes256-ctr\n" +
		"hostkeyalgorithms ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp256,rsa-sha2-512,rsa-sha2-256\n" +
		"hostbasedacceptedalgorithms ssh-ed25519\nstricthostkeychecking ask\n"
	got, err := evaluatedHostKeyAlgorithms([]byte(dump))
	want := []string{"ecdsa-sha2-nistp256-cert-v01@openssh.com", "ecdsa-sha2-nistp256", "rsa-sha2-512", "rsa-sha2-256"}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("members = %v (%v)", got, err)
	}
	for _, test := range []struct {
		name   string
		output string
		want   error
	}{
		{name: "missing", output: "ciphers aes256-ctr\nhostkeyalgorithmsx ssh-ed25519\n", want: errNoAlgorithmList},
		{name: "empty", output: "hostkeyalgorithms\n", want: errUnsupportedAlgorithmList},
		{name: "empty member", output: "hostkeyalgorithms ecdsa-sha2-nistp256,,rsa-sha2-256\n", want: errUnsupportedAlgorithmList},
		{name: "trailing separator", output: "hostkeyalgorithms rsa-sha2-256,\n", want: errUnsupportedAlgorithmList},
		{name: "shell text", output: "hostkeyalgorithms ssh-ed25519;true\n", want: errUnsupportedAlgorithmList},
		{name: "modifier", output: "hostkeyalgorithms -ssh-ed25519 \n", want: errUnsupportedAlgorithmList},
		{name: "space", output: "hostkeyalgorithms ssh-ed25519 rsa-sha2-256\n", want: errUnsupportedAlgorithmList},
		{name: "plus", output: "hostkeyalgorithms +ssh-ed25519\n", want: errUnsupportedAlgorithmList},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := evaluatedHostKeyAlgorithms([]byte(test.output))
			if !errors.Is(err, test.want) || got != nil {
				t.Fatalf("members = %v (%v), want %v", got, err, test.want)
			}
		})
	}
}

func TestTheIntersectionKeepsThePinnedOrder(t *testing.T) {
	rsa := session(machine.IdentityKey)
	rsa.HostKey.Type = "ssh-rsa"
	got, err := admittedAlgorithms(rsa, []string{"ecdsa-sha2-nistp256", "rsa-sha2-256", "rsa-sha2-512"}, PolicyPath)
	if err != nil || got != "rsa-sha2-512,rsa-sha2-256" {
		t.Fatalf("pinned = %q (%v)", got, err)
	}
	_, err = admittedAlgorithms(session(machine.IdentityKey), []string{"ecdsa-sha2-nistp256", "rsa-sha2-256"}, PolicyPath)
	if reported := diagnostics.Of(err); len(reported) != 1 || !strings.Contains(reported[0].Message, "ssh-ed25519") {
		t.Fatalf("excluded = %v", err)
	}
}
