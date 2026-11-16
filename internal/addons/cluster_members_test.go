package addons_test

import (
	"github.com/crmarques/bootwright/internal/containercluster"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/storage"
)

var testClusterMembers = environment.Members(containercluster.MemberMachines, storage.MemberMachines)
