package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io/fs"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/bundlelocal"
	"github.com/crmarques/bootwright/internal/controller/runtimelock"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/dnsserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/ntpserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/proxy"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

const collectionName = "bootwright.core"

//go:embed vulnerability-exceptions.json
var vulnerabilityExceptions []byte

func reviewedExceptions() ([]cli.BOMVulnerability, error) {
	var document struct {
		Format     string `json:"format"`
		Exceptions []struct {
			ID            string   `json:"id"`
			Aliases       []string `json:"aliases"`
			Component     string   `json:"component"`
			State         string   `json:"state"`
			Justification string   `json:"justification"`
			Reason        string   `json:"reason"`
			Expires       string   `json:"expires"`
		} `json:"exceptions"`
	}
	if err := json.Unmarshal(vulnerabilityExceptions, &document); err != nil || document.Format != "bootwright.vulnerability-exceptions-v1" {
		return nil, errors.New("the embedded vulnerability exceptions are no bootwright.vulnerability-exceptions-v1 document")
	}
	exceptions := make([]cli.BOMVulnerability, 0, len(document.Exceptions))
	for _, entry := range document.Exceptions {
		if entry.State != "not_affected" && entry.State != "no_fix" || entry.State == "not_affected" && entry.Justification == "" {
			return nil, errors.New("the embedded vulnerability exception " + entry.ID + " states neither not_affected with a justification nor no_fix")
		}
		exceptions = append(exceptions, cli.BOMVulnerability{ID: entry.ID, Aliases: entry.Aliases, Ref: entry.Component, State: entry.State, Justification: entry.Justification, Reason: entry.Reason, Expires: entry.Expires})
	}
	return exceptions, nil
}

type pinnedImage struct{ consumer, reference string }

func pinnedImages() []pinnedImage {
	return []pinnedImage{
		{"ArtifactServer", artifactserver.DefaultImage()},
		{"DNSServer", dnsserver.Definition(nil).Image},
		{"NTPServer", ntpserver.Definition().Image},
		{"Proxy", proxy.Definition().Image},
		{"emulated BMC", libvirt.EmulatorImage()},
	}
}

func billOfMaterials() (cli.BillOfMaterials, error) {
	stamp, _ := buildStamp()
	return composeBillOfMaterials(buildInformation(), stamp)
}

func composeBillOfMaterials(info cli.BuildInfo, stamp *debug.BuildInfo) (cli.BillOfMaterials, error) {
	carried := cli.BOMProperty{Name: "bootwright:delivery", Value: "carried by the executable"}
	acquired := cli.BOMProperty{Name: "bootwright:delivery", Value: "acquired at use by the pinned digest"}
	if stamp == nil {
		return cli.BillOfMaterials{}, errors.New("the executable carries no build stamp naming its Go modules")
	}
	bom := cli.BillOfMaterials{Name: "bootwright", Version: info.Version, Commit: info.Commit, Source: info.Source, Checked: info.VulnerabilityCheck}
	toolchain := strings.TrimPrefix(runtime.Version(), "go")
	bom.Components = append(bom.Components, cli.BOMComponent{Ref: "go:stdlib@" + toolchain, Type: "library", Name: "stdlib", Version: toolchain, Scope: "required",
		PURL: "pkg:golang/stdlib@" + toolchain, Properties: []cli.BOMProperty{carried}})
	for _, module := range stamp.Deps {
		if module.Replace != nil {
			module = module.Replace
		}
		if module.Version == "" || module.Sum == "" {
			return cli.BillOfMaterials{}, errors.New("the Go module " + module.Path + " is a local replacement with no version or go.sum hash")
		}
		bom.Components = append(bom.Components, cli.BOMComponent{Ref: "go:" + module.Path + "@" + module.Version, Type: "library", Name: module.Path, Version: module.Version, Scope: "required",
			PURL: "pkg:golang/" + module.Path + "@" + module.Version, Properties: []cli.BOMProperty{carried, {Name: "bootwright:go-sum", Value: module.Sum}}})
	}
	lock, err := runtimelock.Embedded()
	if err != nil {
		return cli.BillOfMaterials{}, err
	}
	wheels := map[string][]string{}
	files := map[string]runtimelock.File{}
	for _, abi := range lock.PythonABIs() {
		locked, _ := lock.Files(abi)
		for _, file := range locked {
			files[file.Filename] = file
			wheels[file.Filename] = append(wheels[file.Filename], abi)
		}
	}
	installer := lock.Installer()
	files[installer.Filename] = installer
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for name := range files {
			if !yield(name) {
				return
			}
		}
	}) {
		file := files[name]
		scope, properties := "required", []cli.BOMProperty{carried}
		if _, err := fs.Stat(runtimelock.Carried(), file.Filename); err != nil {
			scope, properties = "optional", []cli.BOMProperty{{Name: "bootwright:delivery", Value: "named by the lock and not carried by this build"}}
		}
		if abis := wheels[name]; len(abis) != 0 {
			properties = append(properties, cli.BOMProperty{Name: "bootwright:python-abi", Value: strings.Join(abis, ",")})
		}
		bom.Components = append(bom.Components, cli.BOMComponent{Ref: "pypi:" + file.Filename, Type: "library", Name: file.Name, Version: file.Version, Scope: scope,
			SHA256: file.SHA256, PURL: "pkg:pypi/" + strings.ReplaceAll(strings.ToLower(file.Name), "_", "-") + "@" + file.Version + "?file_name=" + file.Filename, Distribution: file.URL, Properties: properties})
	}
	bom.Components = append(bom.Components, cli.BOMComponent{Ref: "ansible:" + collectionName, Type: "library", Name: collectionName, Scope: "required",
		Properties: []cli.BOMProperty{carried, {Name: "bootwright:automation-digest", Value: ansible.Digest()}}})
	pins, err := controller.ToolPins()
	if err != nil {
		return cli.BillOfMaterials{}, err
	}
	for _, pin := range pins {
		publisher, err := bundlelocal.PublisherURL(pin)
		if err != nil {
			return cli.BillOfMaterials{}, err
		}
		bom.Components = append(bom.Components, cli.BOMComponent{Ref: "tool:" + pin.Kind + "@" + pin.Default.Version, Type: "application", Name: pin.Kind, Version: pin.Default.Version, Scope: "optional",
			SHA256: pin.Default.SHA256, Distribution: publisher, Properties: []cli.BOMProperty{acquired, {Name: "bootwright:minimum", Value: pin.Minimum}}})
	}
	consumers := map[string][]string{}
	for _, image := range pinnedImages() {
		consumers[image.reference] = append(consumers[image.reference], image.consumer)
	}
	for _, reference := range slices.Sorted(func(yield func(string) bool) {
		for reference := range consumers {
			if !yield(reference) {
				return
			}
		}
	}) {
		repository, digest, found := strings.Cut(reference, "@sha256:")
		name := repository[strings.LastIndex(repository, "/")+1:]
		if !found || len(digest) != 64 || strings.Contains(name, ":") {
			return cli.BillOfMaterials{}, errors.New("the image " + reference + " is not pinned by digest alone")
		}
		bom.Components = append(bom.Components, cli.BOMComponent{Ref: "oci:" + reference, Type: "container", Name: repository, Version: "sha256:" + digest, Scope: "optional",
			SHA256: digest, PURL: "pkg:oci/" + name + "@sha256%3A" + digest + "?repository_url=" + repository,
			Properties: []cli.BOMProperty{acquired, {Name: "bootwright:consumer", Value: strings.Join(consumers[reference], ",")}}})
	}
	if bom.Vulnerabilities, err = reviewedExceptions(); err != nil {
		return cli.BillOfMaterials{}, err
	}
	return bom, nil
}
