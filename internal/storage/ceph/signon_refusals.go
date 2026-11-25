package ceph

func signOnReasons() map[string]error {
	return map[string]error{
		"sign-on-provider-unreachable": cephPrerequisite.Fail("an oauth2-proxy host of a CephSignOn could not read its OIDCProvider's discovery document <issuerURL>/.well-known/openid-configuration over verified TLS, or that document names another issuer, and oauth2-proxy exits when it cannot discover its issuer; Bootwright changed nothing before this check",
			"the native error names the host, the stage (dns, connect, tls, status, content, issuer-mismatch or oversize) and the issuer URL; make the issuer reachable from that host without a proxy, give its certificate chain the anchor the OIDCProvider's spec.trustBundleRef holds (or, without one, the host's system trust store), and make spec.issuerURL the issuer value the provider publishes, then apply again"),
		"sso-unverified": cephPrerequisite.Fail("an oauth2-proxy daemon of a CephSignOn runs with a configuration that skips the identity provider's certificate verification, so Bootwright did not enable the dashboard's single sign-on",
			"the native error names the host, the daemon and where the skip was found; take back what this context owns with bootwright destroy --context <context>, then with bootwright context update select a Ceph image whose cephadm renders oauth2-proxy without that skip, or remove the CephSignOn, then apply"),
		"sign-on-unprotected": cephPrerequisite.Fail("a running management gateway daemon does not render the sign-on corrections this build's gateway-identity record selects (no client cookie forwarded to the dashboard, the Grafana role Viewer), so Bootwright did not enable the dashboard's single sign-on over it",
			"the native error names the host and the correction; the gateway was realized without them, so take back what this context owns with bootwright destroy --context <context>, then apply with this build, which realizes the gateway with them"),
		"sign-on-unenforced": cephNotReady.Fail("the management gateway did not answer an unauthenticated request for its dashboard with a redirect to the identity provider within the readiness bound, so a CephSignOn is not proved enforced",
			"check the gateway and oauth2-proxy daemons with ceph orch ps --daemon_type mgmt-gateway and ceph orch ps --daemon_type oauth2-proxy and the identity provider's authorization endpoint; correct the cause outside Bootwright, then apply again"),
		"sign-on-foreign": cephForeign.Fail("cephadm stores an oauth2-proxy service that this CephSignOn's own records do not prove it applied, so the suspension removed nothing",
			"compare ceph orch ls oauth2-proxy --export with the CephSignOn; restore its frozen document or remove the foreign service outside Bootwright, then repeat bootwright cluster sign-on suspend --context <context> --name <cluster>"),
		"sign-on-suspend-unproved": cephNotReady.Fail("the suspension did not prove within its readiness bound that no oauth2-proxy is stored or runs, the dashboard's single sign-on is off, cephadm configured the gateway and monitoring daemons again and the gateway serves its local sign-in",
			"the native error names what it waited for; check it with ceph orch ps and ceph config get mgr mgr/dashboard/sso_oauth2, then repeat bootwright cluster sign-on suspend --context <context> --name <cluster>, which finishes the same suspension"),
		"sign-on-suspension-drift": cephDrifted.Fail("a suspended CephSignOn's oauth2-proxy service, one of its daemons or the dashboard's single sign-on is back, so the cluster left the suspended state outside Bootwright",
			"suspend it again with bootwright cluster sign-on suspend --context <context> --name <cluster>, or restore the frozen sign-on with bootwright cluster sign-on resume --context <context> --name <cluster>"),
	}
}
