// Command tooltable checks the controller's tool table against each client's
// publisher: verify reads the publisher's checksum and size of every default
// release and its repository's declared license and fails when any differs from
// the table, reads each pinned go.mod of a client release from its publisher
// and fails when its size or SHA-256 differs from its pin or its release is no
// longer a table default, and outdated prints
// each client whose publisher names a newer stable release. Only these
// commands contact a publisher; a build reads the table alone.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/crmarques/bootwright/tools/clientassets"
)

const maxMetadata = 1 << 20

type release struct {
	Version string `json:"version"`
	Bytes   int64  `json:"bytes"`
	SHA256  string `json:"sha256"`
}

type pin struct {
	Kind    string  `json:"kind"`
	Minimum string  `json:"minimum"`
	License string  `json:"license"`
	Default release `json:"default"`
}

type table struct {
	Format string `json:"format"`
	Tools  []pin  `json:"tools"`
}

type fetcher func(method, url string) ([]byte, int64, error)

type publisher struct {
	repository        string
	licenseRepository string
	asset             func(version string) string
	checksum          func(version, asset string) string
	latest            string
}

var publishers = map[string]publisher{
	"govc": {repository: "vmware/govmomi", asset: assetOf("govc"), checksum: func(v, _ string) string {
		return "https://github.com/vmware/govmomi/releases/download/v" + v + "/checksums.txt"
	}},
	"helm":    {repository: "helm/helm", asset: assetOf("helm"), checksum: func(_, asset string) string { return asset + ".sha256sum" }},
	"kubectl": {licenseRepository: "kubernetes/kubernetes", asset: assetOf("kubectl"), checksum: func(_, asset string) string { return asset + ".sha256" }, latest: "https://dl.k8s.io/release/stable.txt"},
	"virtctl": {repository: "kubevirt/kubevirt", asset: assetOf("virtctl")},
}

func assetOf(kind string) func(string) string {
	return func(version string) string {
		asset, _ := clientassets.Of(kind, version)
		return asset.URL
	}
}

var stable = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)$`)

func main() {
	path := flag.String("table", "internal/controller/tooltable.json", "the tool table to check")
	flag.Parse()
	command := flag.Arg(0)
	client := &http.Client{Timeout: 5 * time.Minute}
	fetch := func(method, url string) ([]byte, int64, error) { return fetchHTTP(client, method, url) }
	pins, err := read(*path)
	if err == nil {
		switch command {
		case "verify":
			err = verify(pins, fetch, os.Stdout)
			if err == nil {
				err = verifyModules(pins, fetch, os.Stdout)
			}
		case "outdated":
			err = outdated(pins, fetch, os.Stdout)
		default:
			err = errors.New("usage: tooltable [-table PATH] verify|outdated")
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tooltable:", err)
		os.Exit(1)
	}
}

func read(path string) ([]pin, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var document table
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	kinds := []string{}
	for _, entry := range document.Tools {
		kinds = append(kinds, entry.Kind)
	}
	if document.Format != "bootwright.controller.tool-table-v1" || strings.Join(kinds, ",") != "govc,helm,kubectl,virtctl" {
		return nil, fmt.Errorf("%s is no tool table of format bootwright.controller.tool-table-v1 pinning govc, helm, kubectl and virtctl", path)
	}
	return document.Tools, nil
}

func verify(pins []pin, fetch fetcher, out io.Writer) error {
	failed := false
	for _, entry := range pins {
		digest, size, err := published(entry.Kind, entry.Default.Version, fetch)
		switch {
		case err != nil:
			return fmt.Errorf("%s %s: %w", entry.Kind, entry.Default.Version, err)
		case digest != entry.Default.SHA256 || size != entry.Default.Bytes:
			failed = true
			fmt.Fprintf(out, "%s %s: the publisher names %s and %d bytes; the table %s and %d bytes\n", entry.Kind, entry.Default.Version, digest, size, entry.Default.SHA256, entry.Default.Bytes)
		default:
			fmt.Fprintf(out, "%s %s: matches its publisher\n", entry.Kind, entry.Default.Version)
		}
		declared, err := declaredLicense(entry.Kind, fetch)
		switch {
		case err != nil:
			return fmt.Errorf("%s: %w", entry.Kind, err)
		case declared != entry.License:
			failed = true
			fmt.Fprintf(out, "%s: the publisher's repository declares the license %s; the table %q\n", entry.Kind, declared, entry.License)
		}
	}
	if failed {
		return errors.New("the tool table differs from its publishers")
	}
	return nil
}

func verifyModules(pins []pin, fetch fetcher, out io.Writer) error {
	modules, err := clientassets.Modules()
	if err != nil {
		return err
	}
	return verifyModulePins(pins, modules, fetch, out)
}

func verifyModulePins(pins []pin, modules []clientassets.Module, fetch fetcher, out io.Writer) error {
	failed := false
	for _, module := range modules {
		index := slices.IndexFunc(pins, func(entry pin) bool { return entry.Kind == module.Kind })
		if index < 0 || pins[index].Default.Version != module.Version {
			failed = true
			fmt.Fprintf(out, "%s %s: its go.mod is pinned, but the table's default is another release; move or remove the pin in scripts/tools/clientassets/buildmodules.json\n", module.Kind, module.Version)
			continue
		}
		data, _, err := fetch(http.MethodGet, module.URL)
		if err != nil {
			return fmt.Errorf("%s %s go.mod: %w", module.Kind, module.Version, err)
		}
		sum := sha256.Sum256(data)
		if digest := hex.EncodeToString(sum[:]); digest != module.SHA256 || int64(len(data)) != module.Bytes {
			failed = true
			fmt.Fprintf(out, "%s %s: its release's go.mod is %s and %d bytes; the pin %s and %d bytes\n", module.Kind, module.Version, digest, len(data), module.SHA256, module.Bytes)
			continue
		}
		fmt.Fprintf(out, "%s %s: its go.mod matches its pin\n", module.Kind, module.Version)
	}
	if failed {
		return errors.New("the pinned go.mod files differ from their publishers or the table")
	}
	return nil
}

func declaredLicense(kind string, fetch fetcher) (string, error) {
	source := publishers[kind]
	repository := source.repository
	if source.licenseRepository != "" {
		repository = source.licenseRepository
	}
	data, _, err := fetch(http.MethodGet, "https://api.github.com/repos/"+repository+"/license")
	if err != nil {
		return "", err
	}
	var declared struct {
		License struct {
			ID string `json:"spdx_id"`
		} `json:"license"`
	}
	if err := json.Unmarshal(data, &declared); err != nil || declared.License.ID == "" || declared.License.ID == "NOASSERTION" {
		return "", errors.New("the publisher's repository declares no SPDX license")
	}
	return declared.License.ID, nil
}

func outdated(pins []pin, fetch fetcher, out io.Writer) error {
	for _, entry := range pins {
		newest, err := latest(entry.Kind, fetch)
		if err != nil {
			return fmt.Errorf("%s: %w", entry.Kind, err)
		}
		if newer(newest, entry.Default.Version) {
			fmt.Fprintf(out, "- %s: the default is %s and its publisher's newest stable release %s\n", entry.Kind, entry.Default.Version, newest)
		} else {
			fmt.Fprintf(out, "- %s: the default %s is its publisher's newest stable release\n", entry.Kind, entry.Default.Version)
		}
	}
	return nil
}

func published(kind, version string, fetch fetcher) (string, int64, error) {
	source, found := publishers[kind]
	if !found {
		return "", 0, errors.New("no publisher is known")
	}
	asset := source.asset(version)
	if source.checksum == nil {
		data, _, err := fetch(http.MethodGet, "https://api.github.com/repos/"+source.repository+"/releases/tags/v"+version)
		if err != nil {
			return "", 0, err
		}
		var tag struct {
			Assets []struct {
				URL    string `json:"browser_download_url"`
				Size   int64  `json:"size"`
				Digest string `json:"digest"`
			} `json:"assets"`
		}
		if err := json.Unmarshal(data, &tag); err != nil {
			return "", 0, err
		}
		for _, item := range tag.Assets {
			if item.URL == asset && strings.HasPrefix(item.Digest, "sha256:") {
				return strings.TrimPrefix(item.Digest, "sha256:"), item.Size, nil
			}
		}
		return "", 0, errors.New("the release names no digest for " + asset)
	}
	data, _, err := fetch(http.MethodGet, source.checksum(version, asset))
	if err != nil {
		return "", 0, err
	}
	digest, err := checksum(data, asset[strings.LastIndex(asset, "/")+1:])
	if err != nil {
		return "", 0, err
	}
	_, size, err := fetch(http.MethodHead, asset)
	return digest, size, err
}

func checksum(data []byte, filename string) (string, error) {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	found := ""
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 1 && len(lines) == 1 || len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == filename {
			if found != "" || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(fields[0]) {
				return "", errors.New("the checksum file is malformed or names the asset twice")
			}
			found = fields[0]
		}
	}
	if found == "" {
		return "", errors.New("the checksum file does not name " + filename)
	}
	return found, nil
}

func latest(kind string, fetch fetcher) (string, error) {
	source := publishers[kind]
	if source.latest != "" {
		data, _, err := fetch(http.MethodGet, source.latest)
		return strings.TrimPrefix(strings.TrimSpace(string(data)), "v"), err
	}
	data, _, err := fetch(http.MethodGet, "https://api.github.com/repos/"+source.repository+"/releases/latest")
	if err != nil {
		return "", err
	}
	var tag struct {
		Name string `json:"tag_name"`
	}
	if err := json.Unmarshal(data, &tag); err != nil || !stable.MatchString(tag.Name) {
		return "", errors.New("the publisher's latest release names no stable tag")
	}
	return strings.TrimPrefix(tag.Name, "v"), nil
}

func newer(a, b string) bool {
	left, right := stable.FindStringSubmatch(a), stable.FindStringSubmatch(b)
	if left == nil || right == nil {
		return false
	}
	for index := 1; index <= 3; index++ {
		x, _ := strconv.Atoi(left[index])
		y, _ := strconv.Atoi(right[index])
		if x != y {
			return x > y
		}
	}
	return false
}

func fetchHTTP(client *http.Client, method, url string) ([]byte, int64, error) {
	request, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" && request.URL.Host == "api.github.com" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("%s %s answered %s", method, url, response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxMetadata))
	return data, response.ContentLength, err
}
