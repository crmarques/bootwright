package main

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/acquisition/listing"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller/bundlelocal"
	"github.com/crmarques/bootwright/internal/controller/evidence"
)

func acquisitionTools() listing.Tools {
	return listing.Tools{SourceURL: bundlelocal.SourceURL, Contacts: toolContacts}
}

func toolContacts(request evidence.ToolRequest) ([]listing.Contact, error) {
	contacts, err := bundlelocal.Contacts(request)
	if err != nil {
		return nil, err
	}
	bound := make([]listing.Contact, 0, len(contacts))
	for _, contact := range contacts {
		bound = append(bound, listing.Contact{Host: contact.Host, Purpose: contact.Purpose})
	}
	return bound, nil
}

func listAcquisitions(catalog api.Catalog) ([]cli.Acquisition, error) {
	rows, err := acquisitionTools().List(catalog)
	if err != nil {
		return nil, err
	}
	listed := make([]cli.Acquisition, 0, len(rows))
	for _, row := range rows {
		listed = append(listed, cli.Acquisition(row))
	}
	return listed, nil
}
