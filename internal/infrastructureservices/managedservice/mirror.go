package managedservice

import (
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/acquisition"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type ImageSource struct {
	CredentialsRef  string `json:"credentialsRef,omitempty"`
	CredentialsType string `json:"credentialsType,omitempty"`
	Host            string `json:"host"`
	TrustBundleRef  string `json:"trustBundleRef,omitempty"`
}

func ImageMirror(catalog api.Catalog) (api.Object, bool) {
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return api.Object{}, false
	}
	reference := environments[0].Spec().Get("mirror", "registry", "registryRef")
	if !reference.Present() {
		return api.Object{}, false
	}
	return catalog.Find(api.Registry, reference.Text())
}

func MirroredImage(catalog api.Catalog, reference string) (string, *ImageSource) {
	registry, found := ImageMirror(catalog)
	qualified := reference
	if first, _, _ := strings.Cut(reference, "/"); !strings.ContainsAny(first, ".:") && first != "localhost" {
		qualified = "docker.io/" + reference
	}
	if rest, ok := strings.CutPrefix(qualified, "registry-1.docker.io/"); ok {
		qualified = "docker.io/" + rest
	}
	if origin, _, _ := strings.Cut(qualified, "/"); !found || acquisition.OriginClass(origin) != acquisition.ClassPublic {
		return reference, nil
	}
	location := registry.Spec().Get("url").Text()
	if location == "" {
		return reference, nil
	}
	host, _, _ := strings.Cut(location, "/")
	source := &ImageSource{Host: host, CredentialsRef: registry.Spec().Get("credentialsRef").Text(), TrustBundleRef: registry.Spec().Get("trustBundleRef").Text()}
	if source.CredentialsRef != "" {
		if declaration, found := catalog.Find(api.Secret, source.CredentialsRef); found {
			source.CredentialsType = declaration.Spec().Get("type").Text()
		}
	}
	return location + "/" + qualified, source
}

func (s *ImageSource) SecretReferences() []string {
	if s == nil {
		return nil
	}
	var references []string
	for _, reference := range []string{s.CredentialsRef, s.TrustBundleRef} {
		if reference != "" {
			references = append(references, reference)
		}
	}
	return references
}

func (r Request) Materials(operation string) []lifecycle.MaterialFile {
	var files []lifecycle.MaterialFile
	if operation == "apply" {
		files = append(files, r.ImageSource.Materials()...)
	}
	if operation == "apply" && r.TLS != nil {
		files = append(files,
			lifecycle.MaterialFile{Name: "tls.crt", Part: secrets.CertificatePart, Secret: r.TLS.Secret, Variable: "certificate"},
			lifecycle.MaterialFile{Name: "tls.key", Part: secrets.PrivateKeyPart, Secret: r.TLS.Secret, Variable: "privateKey"})
	}
	if operation != "destroy" && r.CredentialsRef != "" {
		files = append(files,
			lifecycle.MaterialFile{Name: "client-username", Part: secrets.UsernamePart, Secret: r.CredentialsRef, Variable: "clientUsername"},
			lifecycle.MaterialFile{Name: "client-password", Part: secrets.PasswordPart, Secret: r.CredentialsRef, Variable: "clientPassword"})
	}
	return files
}

func (s *ImageSource) Materials() []lifecycle.MaterialFile {
	if s == nil {
		return nil
	}
	var files []lifecycle.MaterialFile
	switch {
	case s.CredentialsRef == "":
	case s.CredentialsType == "dockerConfigJson":
		files = append(files, lifecycle.MaterialFile{Name: "image-auth", Part: secrets.ValuePart, Secret: s.CredentialsRef, Variable: "imageAuth"})
	default:
		files = append(files,
			lifecycle.MaterialFile{Name: "image-username", Part: secrets.UsernamePart, Secret: s.CredentialsRef, Variable: "imageUsername"},
			lifecycle.MaterialFile{Name: "image-password", Part: secrets.PasswordPart, Secret: s.CredentialsRef, Variable: "imagePassword"})
	}
	if s.TrustBundleRef != "" {
		files = append(files, lifecycle.MaterialFile{Name: "image-trust", Part: secrets.CertificatePart, Secret: s.TrustBundleRef, Variable: "imageTrust"})
	}
	return files
}
