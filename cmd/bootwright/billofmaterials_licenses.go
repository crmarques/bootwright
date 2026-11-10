package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"sync"
)

const goLicensesFormat = "bootwright.go-licenses-v1"

//go:embed go-licenses.json
var embeddedGoLicenses []byte

type goLicense struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	License string `json:"license"`
	File    string `json:"file"`
	Text    string `json:"text"`
	Notice  string `json:"notice"`
}

var goLicenses = sync.OnceValues(func() (map[string]goLicense, error) {
	decoder := json.NewDecoder(bytes.NewReader(embeddedGoLicenses))
	decoder.DisallowUnknownFields()
	var document struct {
		Format  string      `json:"format"`
		Modules []goLicense `json:"modules"`
	}
	if err := decoder.Decode(&document); err != nil || document.Format != goLicensesFormat {
		return nil, errors.New("the embedded Go license table is no " + goLicensesFormat + " document")
	}
	table := map[string]goLicense{}
	for _, module := range document.Modules {
		table[module.Path+"@"+module.Version] = module
	}
	return table, nil
})

func goModuleLicense(path, version string) (string, error) {
	table, err := goLicenses()
	if err != nil {
		return "", err
	}
	if path == "stdlib" {
		version = ""
	}
	entry, found := table[path+"@"+version]
	if !found || entry.License == "" || entry.Text == "" {
		return "", errors.New("the Go module " + path + " " + version + " has no license in cmd/bootwright/go-licenses.json; run make licenses")
	}
	return entry.License, nil
}

type imageLicense struct{ license, source string }

var imageLicenses = map[string]imageLicense{
	"registry.access.redhat.com/ubi9/nginx-124": {"BSD-2-Clause", "nginx's own license file, github.com/nginx/nginx LICENSE"},
	"docker.io/ubuntu/squid":                    {"GPL-2.0-or-later", "squid's source headers, \"distributed under GPLv2+ license\""},
	"docker.io/dockurr/chrony":                  {"GPL-2.0-only", "chrony's source headers, \"version 2 of the GNU General Public License\""},
	"docker.io/4km3/dnsmasq":                    {"GPL-2.0-only OR GPL-3.0-only", "dnsmasq's documentation, \"the GPL, version 2 or version 3 at your discretion\""},
	"quay.io/metal3-io/sushy-tools":             {"Apache-2.0", "openstack/sushy-tools LICENSE"},
}
