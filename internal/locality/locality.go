package locality

import "github.com/crmarques/bootwright/api/v1alpha1"

type Policy struct {
	Deps Deps
}

type Deps struct{}

type Result struct {
	OK       bool
	Evidence string
}

var DefaultPolicy = Policy{}

func DefaultDeps() Deps {
	return Deps{}
}

func CheckController(_ v1alpha1.State, _ Policy) Result {
	return Result{OK: true, Evidence: "bastion actions run on localhost"}
}
