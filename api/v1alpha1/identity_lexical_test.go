package v1alpha1

import (
	"strings"
	"testing"
)

func TestIdentityGrammarsAdmitAndRefuseTheirForms(t *testing.T) {
	for _, test := range []struct {
		rule  LexicalRule
		valid []string
		bad   []string
	}{
		{LexicalLDAPURL,
			[]string{"ldaps://dc.example.test", "ldaps://dc.example.test:636", "ldap://dc.example.test", "ldaps://192.0.2.10:3269", "ldaps://[2001:db8::1]:636"},
			[]string{"https://dc.example.test", "LDAPS://dc.example.test", "ldaps://", "ldaps://dc.example.test/", "ldaps://dc.example.test/dc=example", "ldaps://user:pw@dc.example.test", "ldaps://dc.example.test?x", "ldaps://dc.example.test#x", "ldaps://dc.example.test:0", "ldaps://dc.example.test:70000", "ldaps://dc.example.test:", "ldaps://DC.example.test"}},
		{LexicalLDAPDN,
			[]string{"DC=example,DC=test", "OU=Domain Users,DC=example,DC=test", "CN=Zoë,DC=example", strings.Repeat("a", 1024)},
			[]string{strings.Repeat("a", 1025), "DC=example\nDC=test", "DC=\x00", " DC=example", "DC=\xff"}},
		{LexicalLDAPAttribute,
			[]string{"uid", "sAMAccountName", "cn", "x-attr-1"},
			[]string{"", "1uid", "-uid", "sam account", "uid=", "uid;lang-en", "a_b", strings.Repeat("a", 128)}},
		{LexicalLDAPFilter,
			[]string{"(memberOf=CN=ceph,DC=example)", "(&(objectClass=user)(!(userAccountControl:1.2.840.113556.1.4.803:=2)))", "(a=b)(c=d)"},
			[]string{"memberOf=x", "(a=b", "a=b)", "(a=b))(", ")(a=b)(", "(uid=@USERNAME@)", "(a=" + strings.Repeat("b", 1030) + ")", "(a=b)\n", "()x"}},
		{LexicalRealm,
			[]string{"EXAMPLE.TEST", "example.test", "Corp.Example.Test", "corp"},
			[]string{"", "not a domain", "example.test.", "-bad.test", "a..b"}},
	} {
		for _, value := range test.valid {
			if !ValidLexical(test.rule, value) {
				t.Errorf("%s refused %q", test.rule, value)
			}
		}
		for _, value := range test.bad {
			if ValidLexical(test.rule, value) {
				t.Errorf("%s admitted %q", test.rule, value)
			}
		}
	}
}

func TestOIDCIssuerHoldsTheFormTheIssClaimCompares(t *testing.T) {
	for value, want := range map[string]bool{
		"https://sso.example.test":                              true,
		"https://sso.example.test:8443/realms/a":                true,
		"https://sso.example.test/":                             false,
		"https://sso.example.test/realms/a/":                    false,
		"http://sso.example.test":                               false,
		"https://user@sso.example.test":                         false,
		"https://sso.example.test/a?b=c":                        false,
		"https://sso.example.test/a?":                           false,
		"https://sso.example.test/a#b":                          false,
		"https://sso.example.test/" + strings.Repeat("a", 2023): true,
		"https://sso.example.test/" + strings.Repeat("a", 2024): false,
	} {
		if got := ValidOIDCIssuer(value); got != want {
			t.Errorf("ValidOIDCIssuer(%.60q) = %v, want %v", value, got, want)
		}
	}
}
