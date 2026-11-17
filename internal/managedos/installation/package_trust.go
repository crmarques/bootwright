package installation

import "strings"

const packageTrustDirectory = "/tmp/bootwright-package-tree"
const packageTrustPath = packageTrustDirectory + "/ca.pem"
const PackageCertificateToken = "@@BOOTWRIGHT_PACKAGE_TREE_CERTIFICATE@@"

// packageTrustLines stages only the tree listener's bound public certificate
// chain in the installer RAM filesystem. Anaconda runs all pre scripts before
// parsing the source command or starting payload setup. The dedicated CA file
// scopes trust to this source; it never changes the machine's trust store.
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
		return refusal("lifecycle.state", "the HTTPS package source and its installer trust staging disagree", "plan the installation again")
	}
	return nil
}
