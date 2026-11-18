package installation

import "strings"

const packageTrustDirectory = "/tmp/bootwright-package-tree"
const packageTrustPath = packageTrustDirectory + "/ca.pem"
const PackageCertificateToken = "@@BOOTWRIGHT_PACKAGE_TREE_CERTIFICATE@@"

func packageTrustLines(input Installation) []string {
	if !input.PackageTLS {
		return nil
	}
	return []string{
		"%pre --erroronfail --interpreter=/bin/bash",
		"set -euo pipefail",
		"umask 077",
		"mkdir -m 0700 " + packageTrustDirectory,
		"set -C",
		"cat > " + packageTrustPath + " <<'BOOTWRIGHT_PACKAGE_TREE_CA_EOF'",
		PackageCertificateToken,
		"BOOTWRIGHT_PACKAGE_TREE_CA_EOF",
		"test -s " + packageTrustPath,
		"chmod 0600 " + packageTrustPath,
		"%end",
		"",
	}
}

func packageTrustRefusal(input Installation) error {
	https := strings.HasPrefix(input.PackageSource, "url --url=https://")
	scoped := strings.HasSuffix(input.PackageSource, " --sslcacert="+packageTrustPath)
	if input.PackageTLS != https || scoped != https {
		return installRequestInvalid.Fail("the HTTPS package source and its installer trust staging disagree", "plan the installation again")
	}
	return nil
}
