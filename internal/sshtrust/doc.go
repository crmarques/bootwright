// Package sshtrust records and checks SSH server-key trust for declared
// machines: explicit known-hosts material, context-managed trust records,
// and the strict first-use rules (interactive fingerprint confirmation only,
// changed keys fail closed until `host trust --replace`) specified in
// specs/security.md.
package sshtrust
