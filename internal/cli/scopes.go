package cli

import "github.com/crmarques/bootwright/internal/workflow"

type scopeSpec struct {
	name              string
	short             string
	phaseNames        []string
	applyPlaybook     string
	destroyPlaybook   string
	artifactsBaseName string
}

var infraScope = scopeSpec{
	name:              "infra",
	short:             "Install and configure InfraProvider and ClusterInfra",
	phaseNames:        []string{"provider", "cluster"},
	applyPlaybook:     "playbooks/targets/infra/apply.yml",
	destroyPlaybook:   "playbooks/targets/infra/destroy.yml",
	artifactsBaseName: "infra",
}

var clusterScope = scopeSpec{
	name:              "cluster",
	short:             "Install and configure managed OpenShift clusters via openshift-install agent",
	phaseNames:        []string{"clusters"},
	applyPlaybook:     "playbooks/targets/clusters/apply.yml",
	destroyPlaybook:   "playbooks/targets/clusters/destroy.yml",
	artifactsBaseName: "cluster",
}

var extensionsScope = scopeSpec{
	name:              "extensions",
	short:             "Apply post-install cluster extensions",
	phaseNames:        []string{"extensions"},
	artifactsBaseName: "extensions",
}

var allScope = scopeSpec{
	name:              "all",
	short:             "Apply infrastructure, OpenShift clusters, and extensions",
	phaseNames:        []string{"provider", "cluster", "clusters", "extensions"},
	applyPlaybook:     "playbooks/targets/all/apply.yml",
	artifactsBaseName: "all",
}

func (s scopeSpec) phases() []Phase {
	out := make([]Phase, 0, len(s.phaseNames))
	for _, name := range s.phaseNames {
		out = append(out, phases[name])
	}
	return out
}

func (s scopeSpec) applyTarget() workflow.ApplyTarget {
	return workflow.ApplyTarget{Name: s.name, PhaseNames: append([]string(nil), s.phaseNames...)}
}
