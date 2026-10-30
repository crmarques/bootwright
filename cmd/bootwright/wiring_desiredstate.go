package main

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	addonpackages "github.com/crmarques/bootwright/internal/addons/packages"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/containercluster"
	"github.com/crmarques/bootwright/internal/containercluster/openshift"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/customplaybooks"
	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
	"github.com/crmarques/bootwright/internal/substrate"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// wireCompiler binds the syntax adapter, the graph selection functions and each
// context's pure admission rules. Composition supplies the functions; the
// compiler owns phase order and diagnostics.
func wireCompiler() compilation.Compiler { return wireCompilerWithRegistry(addonpackages.Builtin()) }

func wireCompilerWithRegistry(registry addons.Registry) compilation.Compiler {
	members := clusterMembers()
	attachments := func(catalog api.Catalog) []addons.Attachment { return addons.StorageAttachments(catalog, registry) }
	selection := compilation.NewGraphSelector(attachments, func(catalog api.Catalog, attachments []environment.Attachment) environment.Selection {
		return environment.Select(catalog, attachments, members)
	})
	return compilation.NewCompiler(yamlstream.Parser{}, selection.Select, compilation.Rules{Normalize: environment.Normalize, Validate: func(o api.Object, c api.Catalog) []api.Issue {
		return environment.Validate(o, c, members)
	}},
		compilation.Rules{Normalize: func(o api.Object, c api.Catalog) (api.Object, []api.Issue) {
			normalized, issues := secrets.Normalize(o, c)
			return secrets.DeriveCertificateNames(normalized, c, infrastructureservices.CertificateNames, substrate.CertificateNames), issues
		}, ValidateAuthored: secrets.ValidateAuthored, ValidatePartial: secrets.ValidatePartial, Validate: secrets.Validate},
		compilation.Rules{Normalize: func(o api.Object, c api.Catalog) (api.Object, []api.Issue) {
			normalized, issues := addons.Normalize(o, c)
			normalized, packageIssues := addons.NormalizePackages(normalized, registry)
			return normalized, append(issues, packageIssues...)
		}, ValidateAuthored: addons.ValidateAuthored, ValidatePartial: addons.ValidatePartial, Validate: func(o api.Object, c api.Catalog) []api.Issue {
			return addons.ValidateWithRegistry(o, c, registry, addonFacts(c, o.Spec().Get("clusterRef").Text()))
		}},
		compilation.Rules{Normalize: customplaybooks.Normalize, ValidateAuthored: customplaybooks.ValidateAuthored, ValidatePartial: customplaybooks.ValidatePartial, Validate: customplaybooks.Validate},
		compilation.Rules{RetiredField: cephcluster.RetiredField, Normalize: cephcluster.Normalize, ValidateAuthored: cephcluster.ValidateAuthored, ValidatePartial: cephcluster.ValidatePartial, Validate: cephcluster.Validate, Members: storage.MemberMachines},
		compilation.Rules{Normalize: machine.Normalize, NormalizationOrigins: machine.NormalizationOrigins, ValidateAuthored: machine.ValidateAuthored, ValidatePartial: machine.ValidatePartial, Validate: machine.Validate},
		compilation.Rules{Normalize: substrate.Normalize, ValidateAuthored: substrate.ValidateAuthored, ValidatePartial: substrate.ValidatePartial, Validate: substrate.Validate},
		compilation.Rules{Normalize: managedos.Normalize, ValidateAuthored: managedos.ValidateAuthored, ValidatePartial: managedos.ValidatePartial, Validate: managedos.Validate},
		compilation.Rules{Normalize: openshift.Normalize, ValidateAuthored: openshift.ValidateAuthored, ValidatePartial: openshift.ValidatePartial, Validate: func(o api.Object, c api.Catalog) []api.Issue {
			return append(openshift.Validate(o, c), openshift.ValidateNetworkProviders(o, c, registry)...)
		}, Members: containercluster.MemberMachines},
		compilation.Rules{Normalize: infrastructureservices.Normalize, ValidateAuthored: infrastructureservices.ValidateAuthored, ValidatePartial: infrastructureservices.ValidatePartial, Validate: infrastructureservices.Validate})
}

func clusterMembers() func(api.Object) (environment.Membership, bool) {
	return environment.Members(containercluster.MemberMachines, storage.MemberMachines)
}

func addonFacts(c api.Catalog, cluster string) addons.ClusterFacts {
	facts := openshift.AddonFacts(c, cluster)
	for _, hosted := range substrate.HostedNamespaces(c, cluster) {
		facts.HostedNamespaces = append(facts.HostedNamespaces, addons.HostedNamespace{Name: hosted.Name, Labels: hosted.Labels, Conflict: hosted.Conflict})
	}
	for _, export := range cephcluster.AddonExportFacts(c) {
		facts.Exports = append(facts.Exports, addons.ConsumerFacts{Name: export.Name, Cluster: export.Cluster, Gateway: export.Gateway, TLS: export.TLS})
	}
	return facts
}

func wireDesiredState(deps serviceDependencies, compiler compilation.Compiler) cli.DesiredStateService {
	inputs := contexts.Inputs{Repository: deps.Repository, Selection: deps.Selection}
	return compilation.New(inputReader(deps.Files), compiler, inputs)
}

// inputReader reads operator input through the invoking account's opener, and
// binds no opener at all when files is nil rather than a wrapper around nil.
func inputReader(files operatorFiles) inputfs.Reader {
	if files == nil {
		return inputfs.Reader{}
	}
	return inputfs.Reader{Files: inputFiles{files: files}}
}

type inputFiles struct{ files operatorFiles }

func (f inputFiles) Begin(ctx context.Context) (inputfs.FileSession, error) {
	session, err := f.files.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return session, nil
}
