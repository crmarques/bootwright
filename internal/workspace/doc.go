// Package workspace owns context storage: the caller-home context registry
// (~/.bootwright/contexts.yaml) and the root-managed per-context tree under
// /var/lib/bootwright/contexts/<name>/ (copied input, secrets, rendered
// output, runs, clusters). It resolves and prepares those paths for every
// other package; the layout contract lives in specs/security.md.
package workspace
