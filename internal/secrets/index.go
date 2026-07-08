package secret

import (
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// Index is the resolution view of every declared secret: for each name it
// answers where the material lives (context store vs operator file) and, for a
// file source, the on-disk path per material role. It is the single seam
// between the desired-state Secret objects and path resolution, so the resolver
// and every render/preflight consumer depend on Index rather than on where
// secrets happen to be declared.
type Index struct {
	secrets map[string]indexedSecret
	// mode is Environment.spec.secretStorage.mode ("" | source | context); it
	// governs whether file-sourced material is read in place or copied into the
	// per-context store.
	mode string
}

type sourceArm int

const (
	// armContext keeps material only in the per-context store (contextStore
	// source, or an omitted source).
	armContext sourceArm = iota
	// armFile reads material from operator-owned files.
	armFile
	// armGenerated mints material into the per-context store.
	armGenerated
)

type indexedSecret struct {
	arm       sourceArm
	sourceDir string
	// Raw (pre-resolution) operator file paths per material role; populated
	// only for armFile. primaryFile also serves the zero/unknown role.
	primaryFile    string
	tlsKeyFile     string
	sshPrivateFile string
	sshPublicFile  string
}

// NewIndex builds the resolution view from loaded state. The material bytes are
// never read here — only the Secret declarations that say where each secret
// comes from. spec.type fixes which file-source keys populate which role.
func NewIndex(state v1alpha1.State) Index {
	idx := Index{secrets: make(map[string]indexedSecret, len(state.Secrets))}
	if len(state.Environments) > 0 {
		idx.mode = state.Environments[0].Spec.SecretStorage.Mode
	}
	for _, s := range state.Secrets {
		entry := indexedSecret{sourceDir: filepath.Dir(s.SourcePath)}
		switch {
		case s.Spec.Source.Generated != nil:
			entry.arm = armGenerated
		case s.Spec.Source.File != nil:
			entry.arm = armFile
			f := s.Spec.Source.File
			switch s.Spec.Type {
			case v1alpha1.SecretTypeTLSCertificate:
				entry.primaryFile = f.Cert
				entry.tlsKeyFile = f.Key
			case v1alpha1.SecretTypeSSHKeyPair:
				entry.primaryFile = f.PrivateKey
				entry.sshPrivateFile = f.PrivateKey
				if f.PublicKey != "" {
					entry.sshPublicFile = f.PublicKey
				} else {
					entry.sshPublicFile = sshPublicPath(f.PrivateKey)
				}
			default:
				entry.primaryFile = f.Path
			}
		default:
			entry.arm = armContext
		}
		idx.secrets[s.Metadata.Name] = entry
	}
	return idx
}

// useContextPath reports whether a role's material resolves to the per-context
// store rather than an operator file: contextStore and generated secrets always
// live in the store, and context storage mode forces file secrets in too.
func (idx Index) useContextPath(name string, role MaterialRole) bool {
	entry, ok := idx.secrets[name]
	if !ok {
		return false
	}
	if idx.mode == v1alpha1.SecretStorageModeContext {
		return true
	}
	return entry.arm != armFile
}

// sourceFilePath resolves a role's operator-file path, or ("", false) when the
// secret has no file source for that role. The zero/unknown role uses the
// primary file, mirroring the file-source material layout.
func (idx Index) sourceFilePath(name string, role MaterialRole) (string, bool) {
	entry, ok := idx.secrets[name]
	if !ok || entry.arm != armFile {
		return "", false
	}
	raw := entry.primaryFile
	switch role {
	case MaterialTLSKey:
		raw = entry.tlsKeyFile
	case MaterialSSHPublic:
		raw = entry.sshPublicFile
	case MaterialSSHPrivate:
		raw = entry.sshPrivateFile
	}
	if raw == "" {
		return "", false
	}
	path, err := ResolveKeyFilePath(raw, entry.sourceDir)
	return path, err == nil
}

// usesExternalSource reports whether a role reads an operator file (as opposed
// to the per-context store).
func (idx Index) usesExternalSource(name string, role MaterialRole) bool {
	entry, ok := idx.secrets[name]
	if !ok || entry.arm != armFile {
		return false
	}
	return !idx.useContextPath(name, role)
}
