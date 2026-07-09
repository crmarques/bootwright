package secret

import (
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type Index struct {
	secrets map[string]indexedSecret
	mode    string
}

type sourceArm int

const (
	armContext sourceArm = iota
	armFile
	armGenerated
)

type indexedSecret struct {
	arm            sourceArm
	sourceDir      string
	primaryFile    string
	tlsKeyFile     string
	sshPrivateFile string
	sshPublicFile  string
}

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

func (idx Index) usesExternalSource(name string, role MaterialRole) bool {
	entry, ok := idx.secrets[name]
	if !ok || entry.arm != armFile {
		return false
	}
	return !idx.useContextPath(name, role)
}
