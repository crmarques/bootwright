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
		return secretstore.Context{}, nil, diagnostics.NewFailureWithRemediation("context.input", "context "+snapshot.Context.Name+" has no desired state", "",
			"import it with bootwright context update --name "+snapshot.Context.Name+" --input-dir <dir>")
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
	d := secrets.Declaration{Name: object.Name(), Type: spec.Get("type").Text(), Source: "contextStore"}
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
	// Only non-secret declaration data enters these identity digests. The
	// legacy one also covers where the declaration was read from, so a version
	// an earlier build stored stays current while that place is unchanged.
	legacy := d
	legacy.Origin, legacy.Document = origin.Path, origin.Document
	d.LegacyFingerprint, d.LegacyOrigin, d.LegacyDocument = fingerprint(legacy), origin.Path, origin.Document
	d.Fingerprint = fingerprint(d)
	return d
}

func fingerprint(d secrets.Declaration) string {
	d.Fingerprint = ""
	canonical, _ := json.Marshal(d)
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

func validName(name string) bool { return api.ValidLexical("name", name) }

func lookupDeclaration(declarations []secrets.Declaration, name string) (secrets.Declaration, bool) {
	if validName(name) {
		for _, d := range declarations {
			if d.Name == name {
				return d, true
			}
		}
	}
	return secrets.Declaration{}, false
}

// findDeclaration refuses a name the context declares no Secret by, pointing
// at secret check, which lists the Secrets it declares.
func findDeclaration(contextName string, declarations []secrets.Declaration, name string) (secrets.Declaration, error) {
	if d, found := lookupDeclaration(declarations, name); found {
		return d, nil
	}
	message := "Secret " + name + " is not declared in context " + contextName
	if !validName(name) {
		message = "the Secret name is not a valid object name, so it names no Secret declared in context " + contextName
	}
	return secrets.Declaration{}, secrets.Refusal("declaration", message, contextName, name, secrets.Command(contextName, "check"))
}
