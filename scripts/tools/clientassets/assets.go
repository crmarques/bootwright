// Package clientassets names the publisher file of each default client's
// release and, for an archive, the member holding the executable. The tool
// table checker and the vulnerability gate share it, and the gate verifies
// every download against the table's size and SHA-256, so a wrong name fails
// loudly instead of passing.
package clientassets

// Asset is one publisher file. Member is empty for a bare executable and the
// archive member's base name for a tar.gz.
type Asset struct{ URL, Member string }

// Of returns the asset of a client's release, written MAJOR.MINOR.PATCH.
func Of(kind, version string) (Asset, bool) {
	switch kind {
	case "govc":
		return Asset{URL: "https://github.com/vmware/govmomi/releases/download/v" + version + "/govc_Linux_x86_64.tar.gz", Member: "govc"}, true
	case "helm":
		return Asset{URL: "https://get.helm.sh/helm-v" + version + "-linux-amd64.tar.gz", Member: "helm"}, true
	case "kubectl":
		return Asset{URL: "https://dl.k8s.io/release/v" + version + "/bin/linux/amd64/kubectl"}, true
	case "virtctl":
		return Asset{URL: "https://github.com/kubevirt/kubevirt/releases/download/v" + version + "/virtctl-v" + version + "-linux-amd64"}, true
	}
	return Asset{}, false
}
