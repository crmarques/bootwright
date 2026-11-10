package main

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

type answer struct {
	data string
	size int64
}

func fake(answers map[string]answer) fetcher {
	return func(method, url string) ([]byte, int64, error) {
		value, found := answers[method+" "+url]
		if !found {
			return nil, 0, errors.New("unexpected " + method + " " + url)
		}
		return []byte(value.data), value.size, nil
	}
}

func TestVerifyComparesEachDefaultWithItsPublisher(t *testing.T) {
	digest := strings.Repeat("a", 64)
	answers := map[string]answer{
		"GET https://get.helm.sh/helm-v4.3.1-linux-amd64.tar.gz.sha256sum":        {data: digest + "  helm-v4.3.1-linux-amd64.tar.gz\n"},
		"HEAD https://get.helm.sh/helm-v4.3.1-linux-amd64.tar.gz":                 {size: 20},
		"GET https://api.github.com/repos/helm/helm/license":                      {data: `{"license":{"spdx_id":"Apache-2.0"}}`},
		"GET https://api.github.com/repos/kubevirt/kubevirt/license":              {data: `{"license":{"spdx_id":"Apache-2.0"}}`},
		"GET https://api.github.com/repos/kubevirt/kubevirt/releases/tags/v1.9.1": {data: `{"assets":[{"browser_download_url":"https://github.com/kubevirt/kubevirt/releases/download/v1.9.1/virtctl-v1.9.1-linux-amd64","size":30,"digest":"sha256:` + digest + `"}]}`},
	}
	pins := []pin{{Kind: "helm", License: "Apache-2.0", Default: release{Version: "4.3.1", Bytes: 20, SHA256: digest}}, {Kind: "virtctl", License: "Apache-2.0", Default: release{Version: "1.9.1", Bytes: 30, SHA256: digest}}}
	var out strings.Builder
	if err := verify(pins, fake(answers), &out); err != nil {
		t.Fatalf("a matching table failed: %v\n%s", err, out.String())
	}
	pins[0].Default.Bytes = 21
	out.Reset()
	if err := verify(pins, fake(answers), &out); err == nil || !strings.Contains(out.String(), "helm 4.3.1: the publisher names") {
		t.Fatalf("a size the publisher does not name passed: %v\n%s", err, out.String())
	}
	pins[0].Default.Bytes = 20
	pins[1].License = "MIT"
	out.Reset()
	if err := verify(pins, fake(answers), &out); err == nil || !strings.Contains(out.String(), "virtctl: the publisher's repository declares the license Apache-2.0; the table \"MIT\"") {
		t.Fatalf("a license the publisher does not declare passed: %v\n%s", err, out.String())
	}
	answers["GET https://api.github.com/repos/helm/helm/license"] = answer{data: `{"license":{"spdx_id":"NOASSERTION"}}`}
	if err := verify(pins, fake(answers), &out); err == nil || !strings.Contains(err.Error(), "declares no SPDX license") {
		t.Fatalf("a repository declaring no license passed: %v", err)
	}
}

func TestAChecksumFileMustNameTheAssetOnce(t *testing.T) {
	digest := strings.Repeat("b", 64)
	if found, err := checksum([]byte(digest+"  wanted\n"+strings.Repeat("c", 64)+"  other\n"), "wanted"); err != nil || found != digest {
		t.Fatalf("checksum = %q, %v", found, err)
	}
	for _, data := range []string{digest + "  wanted\n" + digest + "  wanted\n", "short  wanted\n", digest + "  other\n"} {
		if _, err := checksum([]byte(data), "wanted"); err == nil {
			t.Errorf("%q was accepted", data)
		}
	}
}

func TestOutdatedNamesANewerStableRelease(t *testing.T) {
	answers := map[string]answer{
		http.MethodGet + " https://dl.k8s.io/release/stable.txt":                        {data: "v1.38.0\n"},
		http.MethodGet + " https://api.github.com/repos/vmware/govmomi/releases/latest": {data: `{"tag_name":"v0.56.2"}`},
	}
	var out strings.Builder
	if err := outdated([]pin{{Kind: "kubectl", Default: release{Version: "1.37.2"}}, {Kind: "govc", Default: release{Version: "0.56.2"}}}, fake(answers), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "kubectl: the default is 1.37.2 and its publisher's newest stable release 1.38.0") || !strings.Contains(out.String(), "govc: the default 0.56.2 is its publisher's newest stable release") {
		t.Fatal(out.String())
	}
}
