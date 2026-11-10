package registry

import (
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/secrets"
)

const Implementation = "registry-distribution-v1"

const Kind = string(api.Registry)

// defaultImage is CNCF Distribution 3.1.2, pinned by its multi-architecture
// index digest.
const defaultImage = "docker.io/library/registry@sha256:38907510915c3f1604c7e83d6ab602ab3020d092f73bdaed8696773693ff768c"

// Definition binds the shared managed-service behavior to Distribution, which
// serves HTTPS alone from its declared certificate, authenticates every push
// and pull against a bcrypt password file derived from the declared credential
// on the controller, and keeps what it stores under its content root until destroy.
type PasswordFile func(username, password []byte) ([]byte, error)

func Definition(passwordFile PasswordFile) managedservice.Definition {
	return managedservice.Definition{
		Kind:           api.Registry,
		Implementation: Implementation,
		Selects:        "distribution",
		Version:        managedservice.RequestVersion,
		Slug:           "registry",
		Variable:       managedservice.Variable,
		Purpose:        "serve container images",
		Subject:        "registry",
		Image:          defaultImage,
		Extend:         extend,
		Certificate:    certificate,
		PasswordFile:   passwordFile,
	}
}

func extend(catalog api.Catalog, spec api.Value, request *managedservice.Request) error {
	tls := spec.Get("tls", "secretRef").Text()
	credential := spec.Get("credentialsRef").Text()
	if tls == "" || credential == "" {
		return managedservice.Refusal("api.invariant", "a managed Registry needs spec.tls.secretRef and spec.credentialsRef", "set both on Registry/"+request.Identity.Service)
	}
	if declaration, found := catalog.Find(api.Secret, credential); !found || declaration.Spec().Get("type").Text() != "usernamePassword" {
		return managedservice.Refusal("api.invariant", "a managed Registry authenticates its clients with a usernamePassword Secret, which "+credential+" is not",
			"set spec.credentialsRef on Registry/"+request.Identity.Service+" to a usernamePassword Secret")
	}
	minimum := spec.Get("tls", "minVersion").Text()
	if minimum == "" {
		minimum = "TLSv1.2"
	}
	request.TLS = &managedservice.TLS{MinVersion: minimum, Secret: tls}
	request.CredentialsRef = credential
	return nil
}

func certificate(material map[string]secrets.Material, secret, contextName string, addresses []string, now time.Time, registered bool) (string, error) {
	bound, err := artifactserver.BoundServingMaterial(material, secret, contextName)
	if err != nil {
		return "", err
	}
	if !registered {
		return "", artifactserver.ProveServingCertificate(bound, secret, contextName, addresses, now)
	}
	served, err := artifactserver.ValidateServingCertificate(bound, secret, contextName, addresses, now)
	return served.Fingerprint, err
}
