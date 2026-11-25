package cephcluster

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func directoryServer(name string, realm string) api.Object {
	spec := m("management", "external", "urls", api.StringList("ldaps://dc.corp.example.test"), "bindDN", "CN=svc ceph,OU=Service Accounts,DC=corp,DC=example,DC=test", "bindPasswordRef", "smb-pass", "users", m("baseDN", "DC=corp,DC=example,DC=test", "nameAttribute", "sAMAccountName"))
	if realm != "" {
		spec = spec.With("activeDirectory", m("realm", realm))
	}
	return object(api.LDAPServer, name, spec)
}

func smbDirectoryCatalog(t *testing.T) api.Catalog {
	t.Helper()
	catalog := smbCatalog(t)
	return replace(replace(replace(catalog, directoryServer("corp", "CORP.EXAMPLE.TEST")), directoryServer("plain", "")), directoryServer("other", "OTHER.EXAMPLE.TEST"))
}

func smbADCluster(t *testing.T, catalog api.Catalog, changes ...func(api.Value) api.Value) (api.Object, api.Catalog) {
	t.Helper()
	join := []api.Value{m("source_type", "resource", "ref", "join")}
	base := smbChain(smbSpec("resourceRefs", nil), smbNative("auth_mode", "active-directory", "user_group_settings", nil, "domain_settings", m("join_sources", join)), smbSpec("ldapServerRef", "corp"))
	cluster, changed := smbWith(t, catalog, "smb-files", smbChain(append([]func(api.Value) api.Value{base}, changes...)...))
	cluster, _ = Normalize(cluster, changed)
	return cluster, replace(changed, cluster)
}

func TestAnADSMBClusterDerivesItsRealmFromItsDirectory(t *testing.T) {
	cluster, catalog := smbADCluster(t, smbDirectoryCatalog(t))
	if got := textOf(cluster.Spec(), "native", "domain_settings", "realm"); got != "CORP.EXAMPLE.TEST" {
		t.Fatalf("derived realm = %q, want CORP.EXAMPLE.TEST", got)
	}
	if got := textOf(cluster.Spec(), "ldapServerRef"); got != "corp" {
		t.Fatalf("effective ldapServerRef = %q", got)
	}
	if got := cluster.Spec().Get("resourceRefs").Strings(); len(got) != 1 || got[0] != "smb-join" {
		t.Fatalf("resourceRefs = %q", got)
	}
	requireAdmits(t, cluster, catalog)
}

func TestAnADSMBClusterRealmAgreesWithItsDirectoryWithoutCase(t *testing.T) {
	join := []api.Value{m("source_type", "resource", "ref", "join")}
	for _, realm := range []string{"corp.example.test", "Corp.Example.Test", "CORP.EXAMPLE.TEST"} {
		cluster, catalog := smbADCluster(t, smbDirectoryCatalog(t), smbNative("domain_settings", m("realm", realm, "join_sources", join)))
		requireAdmits(t, cluster, catalog)
		if got := textOf(cluster.Spec(), "native", "domain_settings", "realm"); got != realm {
			t.Fatalf("an authored realm %q was rewritten to %q", realm, got)
		}
	}
	cluster, catalog := smbADCluster(t, smbDirectoryCatalog(t), smbNative("domain_settings", m("realm", "other.example.test", "join_sources", join)))
	issues := Validate(cluster, catalog)
	exactIssue(t, issues, "api.invariant", "$.spec.native.domain_settings.realm", "the realm disagrees with the Active Directory realm of LDAPServer corp", "omit domain_settings.realm so it derives from LDAPServer corp, or write the realm that server declares (in any case)")
	if len(issues) != 1 || strings.Contains(issues[0].Message+issues[0].Remediation, "example.test") {
		t.Fatalf("the refusal echoed a realm or added issues: %#v", issues)
	}
}

func TestAnADSMBClusterNamesADirectoryThatDeclaresADomain(t *testing.T) {
	cluster, catalog := smbADCluster(t, smbDirectoryCatalog(t), smbSpec("ldapServerRef", "plain"))
	issues := Validate(cluster, catalog)
	exactIssue(t, issues, "api.invariant", "$.spec.ldapServerRef", "LDAPServer plain declares no spec.activeDirectory, so it is not an Active Directory domain an SMB cluster can join", "declare spec.activeDirectory.realm on LDAPServer plain, or remove spec.ldapServerRef")
	if len(issues) != 2 {
		t.Fatalf("a directory without a domain also leaves the realm missing at its own field: %#v", issues)
	}
	requireIssue(t, issues, "$.spec.native.domain_settings.realm")
}

func TestSMBLDAPServerRefRefusesOutsideAnADCluster(t *testing.T) {
	catalog := smbDirectoryCatalog(t)
	const message = "only a ceph.smb.cluster resource joins a directory"
	for _, name := range []string{"smb-users", "smb-join", "smb-docs"} {
		full, partial := smbBoth(t, catalog, name, smbSpec("ldapServerRef", "corp"))
		exactIssue(t, full, "api.invariant", "$.spec.ldapServerRef", message, "remove spec.ldapServerRef")
		exactIssue(t, partial, "api.invariant", "$.spec.ldapServerRef", message, "remove spec.ldapServerRef")
	}
	full, partial := smbBoth(t, catalog, "smb-files", smbSpec("ldapServerRef", "corp"))
	exactIssue(t, full, "api.invariant", "$.spec.ldapServerRef", "a user-mode SMB cluster joins no directory", "remove spec.ldapServerRef, or set auth_mode: active-directory")
	exactIssue(t, partial, "api.invariant", "$.spec.ldapServerRef", "a user-mode SMB cluster joins no directory", "remove spec.ldapServerRef, or set auth_mode: active-directory")
}

func TestAnADSMBClusterWithoutADirectoryStillNeedsItsDomainSettings(t *testing.T) {
	catalog := smbDirectoryCatalog(t)
	cluster, changed := smbWith(t, catalog, "smb-files", smbChain(smbSpec("resourceRefs", nil), smbNative("auth_mode", "active-directory", "user_group_settings", nil)))
	exactIssue(t, Validate(cluster, changed), "api.invariant", "$.spec.native.domain_settings", smbADSettings, smbADSettingsFix)
}

func TestAnADSMBClusterDerivedRealmStillNeedsJoinSources(t *testing.T) {
	cluster, catalog := smbADCluster(t, smbDirectoryCatalog(t), smbNative("domain_settings", nil))
	if got := textOf(cluster.Spec(), "native", "domain_settings", "realm"); got != "CORP.EXAMPLE.TEST" {
		t.Fatalf("derived realm = %q", got)
	}
	requireIssue(t, Validate(cluster, catalog), "$.spec.native.domain_settings.join_sources")
}
