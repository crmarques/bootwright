package agentinstall

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/material"
)

func TestFeatureCredentialsAreLentOnlyForMediaApply(t *testing.T) {
	for _, kind := range []string{"usernamePassword", "dockerConfigJson"} {
		t.Run(kind, func(t *testing.T) {
			execution, request := mediaExecution(t, testDigest)
			request.RegistryMirror = &RegistryMirror{Host: "mirror.example.test", CredentialsRef: "mirror-auth", CredentialsType: kind}
			request.InstallProxy = &InstallProxy{HTTPProxy: "http://proxy.example.test:3128", HTTPSProxy: "http://proxy.example.test:3128", CredentialsRef: "proxy-auth"}
			execution.Material["mirror-auth"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("mirror-user"), secrets.PasswordPart: []byte("sensitive-sentinel"), secrets.ValuePart: []byte(`{"auths":{"mirror.example.test":{"auth":"sensitive-sentinel"}}}`)})
			execution.Material["proxy-auth"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("proxy-user"), secrets.PasswordPart: []byte("sensitive-sentinel")})
			canonical, err := request.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			execution.Block.Request = canonical
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: mediaEvidence(t, testDigest, nil)}}
			capability := NewMedia(runner, material.AuthorizedKey)
			if _, err := capability.Apply(context.Background(), execution); err != nil {
				t.Fatal(err)
			}
			run := runner.requests[0]
			if strings.Contains(string(run.Canonical), "sensitive-sentinel") {
				t.Fatal("secret frozen")
			}

			want := map[string][]secrets.Part{"proxy-auth": {secrets.PasswordPart, secrets.UsernamePart}}
			if kind == "usernamePassword" {
				want["mirror-auth"] = []secrets.Part{secrets.PasswordPart, secrets.UsernamePart}
			} else {
				want["mirror-auth"] = []secrets.Part{secrets.ValuePart}
			}
			for name, parts := range want {
				got := run.Material[name].Parts()
				if len(got) != len(parts) {
					t.Fatalf("lent %s = %v", name, got)
				}
				for i := range parts {
					if got[i] != parts[i] {
						t.Fatalf("lent %s = %v", name, got)
					}
				}
			}
			if _, err := capability.run(context.Background(), execution, "observe", request); err != nil {
				t.Fatal(err)
			}
			if len(runner.requests[1].Materials) != 0 {
				t.Fatal("observation reads private feature material")
			}
		})
	}
}
