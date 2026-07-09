package inventory

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/proxy"
)

func TestEffectiveProxyVarsTrustBundleRef(t *testing.T) {
	withCA := effectiveProxyVars(&proxy.Effective{
		HTTP:        "http://proxy:3128",
		Auth:        v1alpha1.SecretRef{Name: "proxy-auth"},
		TrustBundle: v1alpha1.SecretRef{Name: "corporate-proxy-ca"},
	})
	if got := withCA["trustBundleRef"]; got != "corporate-proxy-ca" {
		t.Fatalf("trustBundleRef = %v, want corporate-proxy-ca", got)
	}

	withoutCA := effectiveProxyVars(&proxy.Effective{HTTP: "http://proxy:3128"})
	if _, ok := withoutCA["trustBundleRef"]; ok {
		t.Fatalf("trustBundleRef present when the proxy declares none")
	}
}
