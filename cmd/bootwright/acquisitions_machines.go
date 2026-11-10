package main

import (
	"net/url"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli"
)

const (
	hostedRegistrationHost = "https://subscription.rhsm.redhat.com"
	hostedContentHost      = "https://cdn.redhat.com"
)

func machineRepositoryAcquisitions(catalog api.Catalog) []cli.Acquisition {
	installed := map[string][]string{}
	for _, machine := range catalog.OfKind(api.Machine) {
		if profile := machine.Spec().Get("os", "installProfileRef").Text(); profile != "" {
			installed[profile] = append(installed[profile], machine.Name())
		}
	}
	var rows []cli.Acquisition
	for _, profile := range catalog.OfKind(api.MachineInstallProfile) {
		machines := installed[profile.Name()]
		if len(machines) == 0 {
			continue
		}
		spec := profile.Spec()
		version := spec.Get("os", "version").Text()
		if entitlement, found := catalog.Find(api.Entitlement, spec.Get("subscription", "entitlementRef").Text()); found {
			rows = append(rows, registrationAcquisitions(entitlement, version, machines, profile.Identity())...)
		}
		for _, repository := range spec.Get("customizations", "repositories", "configure").Items() {
			if enabled := repository.Get("enabled"); enabled.Present() && !enabled.Bool() {
				continue
			}
			id, source := repository.Get("id").Text(), repository.Get("baseURL").Text()
			rows = append(rows, cli.Acquisition{Kind: "machine repository", Artifact: id, Version: version, Origin: source, Class: originClass(source), Machines: machines, Consumer: profile.Identity()})
			if key := repository.Get("gpgKeyURL").Text(); key != "" && repository.Get("gpgCheck").Bool() && remoteLocation(key) {
				rows = append(rows, cli.Acquisition{Kind: "machine repository key", Artifact: id, Origin: redactedLocation(key), Class: originClass(key), Machines: machines, Consumer: profile.Identity()})
			}
		}
	}
	return rows
}

func registrationAcquisitions(entitlement api.Object, version string, machines []string, consumer string) []cli.Acquisition {
	rhsm := entitlement.Spec().Get("rhsm")
	if entitlement.Spec().Get("type").Text() != "redhatRhel" || rhsm.Get("management").Text() != "managed" {
		return nil
	}
	registration, content := hostedRegistrationHost, hostedContentHost
	if satellite := rhsm.Get("satellite"); satellite.Present() {
		registration, content = "https://"+satellite.Get("hostname").Text(), satellite.Get("contentBaseURL").Text()
	}
	return []cli.Acquisition{
		{Kind: "machine registration", Artifact: "subscription registration", Origin: registration, Class: originClass(registration), Machines: machines, Consumer: consumer},
		{Kind: "machine repositories", Artifact: "subscription repositories", Version: version, Origin: content, Class: originClass(content), Machines: machines, Consumer: consumer},
	}
}

func redactedLocation(location string) string {
	parsed, err := url.Parse(location)
	if err != nil {
		return ""
	}
	parsed.User, parsed.RawQuery, parsed.Fragment = nil, "", ""
	return parsed.String()
}

func remoteLocation(location string) bool {
	parsed, err := url.Parse(location)
	return err == nil && parsed.Host != "" && parsed.Scheme != "file"
}
