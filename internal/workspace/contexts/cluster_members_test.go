package contexts_test

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/storage"
)

var testClusterMembers = environment.Members(containercluster.MemberMachines, storage.MemberMachines)

func testEnvironmentValidate(object api.Object, catalog api.Catalog) []api.Issue {
	return environment.Validate(object, catalog, testClusterMembers)
}
