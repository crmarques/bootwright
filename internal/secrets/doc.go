// Package secret owns the context secret store: declared-secret resolution,
// AES-256-GCM envelope encryption with the root-owned-file keyring,
// generated credentials/certificates/SSH key pairs, and short-lived plaintext
// materialization for external tools under per-run runtime directories. The
// storage and permission contract lives in specs/security.md.
package secret
