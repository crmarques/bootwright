package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/environment/inspection"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

const revealMarker = "reveal-marker-bytes"

type revealCase struct {
	name   string
	args   []string
	result func(material secrets.Material) commandResult
}

func revealCases() []revealCase {
	return []revealCase{
		{"secret show", []string{"secret", "show", "--name", "sample", "--part", "value"}, func(m secrets.Material) commandResult {
			return commandResult{secretReveal: &custody.RevealResult{Material: m, Part: secrets.ValuePart}}
		}},
		{"cluster kubeconfig", []string{"cluster", "kubeconfig", "--name", "edge"}, func(m secrets.Material) commandResult {
			return commandResult{kubeconfig: &containeraccess.KubeconfigResult{Material: m}}
		}},
		{"cluster keyring", []string{"cluster", "keyring", "--name", "storage", "--client", "vm-disks"}, func(m secrets.Material) commandResult {
			return commandResult{keyring: &inspection.KeyringResult{Material: m}}
		}},
	}
}

func refusedRevealMaterials() map[string]func() secrets.Material {
	return map[string]func() secrets.Material{
		"oversized": func() secrets.Material {
			return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: append([]byte(revealMarker), bytes.Repeat([]byte("k"), secrets.MaxPartBytes)...)})
		},
		"two parts": func() secrets.Material {
			return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(revealMarker), secrets.PasswordPart: []byte("x")})
		},
		"absent part": func() secrets.Material {
			return secrets.NewMaterial(map[secrets.Part][]byte{secrets.PasswordPart: []byte(revealMarker)})
		},
	}
}

func TestStructuredRevealFailsInsteadOfFallingToTheHumanWriter(t *testing.T) {
	for _, reveal := range revealCases() {
		for name, material := range refusedRevealMaterials() {
			t.Run(reveal.name+"/"+name, func(t *testing.T) {
				if reveal.name == "secret show" && name == "two parts" {
					t.Skip("a Secret reveal selects one part of a multi-part material")
				}
				root, err := newCommandTree(New(Config{}))
				if err != nil {
					t.Fatal(err)
				}
				command, _, err := root.Find(reveal.args[:2])
				if err != nil || command.ParseFlags(append(reveal.args[2:], "--output", "json")) != nil {
					t.Fatalf("command %v: %v", reveal.args, err)
				}
				var out bytes.Buffer
				runner := New(Config{Out: &out, ErrOut: &out})
				path := strings.Join(reveal.args[:2], " ")
				handled, err := specFor(path).render.structured(runner, renderCall{ctx: context.Background(), command: command, path: path, result: reveal.result(material())})
				var failure *resultFailure
				if !handled || !errors.As(err, &failure) || failure.code != "runtime.internal" || out.Len() != 0 {
					t.Fatalf("structured form handled=%t err=%v wrote %q; want a result failure and no bytes", handled, err, out.String())
				}
			})
		}
	}
}

func TestRefusedStructuredRevealWritesNoSecretBytesAndFails(t *testing.T) {
	for _, reveal := range revealCases() {
		for name, material := range refusedRevealMaterials() {
			for _, format := range []string{"json", "yaml"} {
				t.Run(reveal.name+"/"+name+"/"+format, func(t *testing.T) {
					if reveal.name == "secret show" && name == "two parts" {
						t.Skip("a Secret reveal selects one part of a multi-part material")
					}
					record := &dispatchRecord{result: reveal.result(material())}
					var out, errOut bytes.Buffer
					code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), append([]string{"--output", format}, reveal.args...))
					if code != 1 || strings.Contains(out.String()+errOut.String(), revealMarker) || !strings.Contains(out.String(), "runtime.internal") {
						t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
					}
				})
			}
		}
	}
}

func TestStructuredSecretRevealRefusesAPartThePlainRevealRefuses(t *testing.T) {
	material := secrets.NewMaterial(map[secrets.Part][]byte{secrets.PasswordPart: []byte(revealMarker)})
	record := &dispatchRecord{result: commandResult{secretReveal: &custody.RevealResult{Material: material, Part: secrets.PasswordPart}}}
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"--output", "json", "secret", "show", "--name", "sample", "--part", "value"})
	if code != 1 || strings.Contains(out.String()+errOut.String(), revealMarker) {
		t.Fatalf("a structured reveal of a part other than the requested one: code=%d stdout=%q", code, out.String())
	}
}
