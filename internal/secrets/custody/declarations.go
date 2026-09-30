package custody

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func (s Service) resolve(ctx context.Context, name string) (secretstore.Context, []secrets.Declaration, error) {
	if err := ctx.Err(); err != nil {
		return secretstore.Context{}, nil, err
	}
	if s.access == nil || s.compiler == nil || s.material == nil {
		return secretstore.Context{}, nil, secretstore.Failure("store.implementation", "secret service is not configured")
	}
	snapshot, err := s.access.Context(ctx, name)
	if err != nil {
		return secretstore.Context{}, nil, err
	}
	if snapshot.Context.Revision == "" {
		return secretstore.Context{}, nil, diagnostics.NewFailure("context.input", "context has no desired state; run context update --name "+snapshot.Context.Name+" --input-dir <dir>", "")
	}
	state, _, err := s.compiler.Compile(ctx, snapshot.Inputs)
	if err != nil {
		return secretstore.Context{}, nil, err
	}
	if state == nil {
		return secretstore.Context{}, nil, secretstore.Failure("declaration", "secret declarations could not be compiled")
	}
	declarations := []secrets.Declaration{}
	for _, object := range state.Effective().Objects() {
		if object.Kind() != api.Secret {
			continue
		}
		origin, ok := state.Origin(object.Identity())
		if !ok {
			return secretstore.Context{}, nil, secretstore.Failure("declaration", "secret declaration provenance is unavailable")
		}
		declarations = append(declarations, declarationOf(object, origin))
	}
	slices.SortFunc(declarations, func(a, b secrets.Declaration) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return snapshot.Context, declarations, nil
}

func declarationOf(object api.Object, origin diagnostics.SourceLocation) secrets.Declaration {
	spec := object.Spec()
	d := secrets.Declaration{Name: object.Name(), Type: spec.Get("type").Text(), Source: "contextStore", Origin: origin.Path, Document: origin.Document}
	if g := spec.Get("source", "generated"); g.Present() {
		d.Source = "generated"
		d.Generation = secrets.Generation{Username: g.Get("username").Text(), CommonName: g.Get("commonName").Text(), DNSNames: g.Get("dnsNames").Strings(), IPAddresses: g.Get("ipAddresses").Strings(), KeyType: g.Get("keyType").Text(), Comment: g.Get("comment").Text()}
		validity, _ := g.Get("validityDays").Int64()
		d.Generation.ValidityDays = int(validity)
		entropy, ok := g.Get("bytes").Int64()
		if !ok && d.Type == "token" {
			entropy = 32
		}
		d.Generation.Bytes = int(entropy)
	}
	// Only non-secret declaration data enters this identity digest.
	canonical, _ := json.Marshal(d)
	digest := sha256.Sum256(canonical)
	d.Fingerprint = hex.EncodeToString(digest[:])
	return d
}

func validName(name string) bool { return api.ValidLexical("name", name) }
func findDeclaration(declarations []secrets.Declaration, name string) (secrets.Declaration, error) {
	if validName(name) {
		for _, d := range declarations {
			if d.Name == name {
				return d, nil
			}
		}
	}
	return secrets.Declaration{}, secretstore.Failure("declaration", "name must identify an effective Secret declaration")
}

func validateInput(kind string, input secrets.Input) error {
	if input.Provided & ^secrets.AllowedInputFields(kind) != 0 {
		return secretstore.Failure("input", "an explicitly supplied input flag is not applicable to the declared secret type")
	}
	value := (input.ValueFile != "") != input.ValueStdin
	password := (input.PasswordFile != "") != input.PasswordStdin
	noValue := input.ValueFile == "" && !input.ValueStdin
	noPassword := input.Username == "" && input.PasswordFile == "" && !input.PasswordStdin
	noKeys := input.CertificateFile == "" && input.PrivateKeyFile == "" && input.PublicKeyFile == ""
	valid := false
	switch kind {
	case "opaque", "token", "dockerConfigJson":
		valid = value && noPassword && noKeys
	case "usernamePassword":
		valid = password && input.Username != "" && noValue && noKeys
	case "caBundle":
		valid = input.CertificateFile != "" && input.PrivateKeyFile == "" && input.PublicKeyFile == "" && noValue && noPassword
	case "tlsCertificate":
		valid = input.CertificateFile != "" && input.PrivateKeyFile != "" && input.PublicKeyFile == "" && noValue && noPassword
	case "sshKeyPair":
		valid = input.PrivateKeyFile != "" && input.CertificateFile == "" && noValue && noPassword
	}
	if !valid {
		return secretstore.Failure("source", "input flags do not match the declared secret type")
	}
	return nil
}
