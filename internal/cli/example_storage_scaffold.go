package cli

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/crmarques/bootwright/internal/state/scaffold"
)

const (
	exampleKindContainerCluster = "container-cluster"
	exampleKindStorageCluster   = "storage-cluster"
)

func exampleKinds() []string {
	return []string{exampleKindContainerCluster, exampleKindStorageCluster}
}

const storageExampleApplySupport = "supported - provided machines with a cephadm-managed Ceph cluster"

type storageExampleNode struct {
	Machine string
	Node    string
	Address string
	Roles   []string
	Devices []string
}

type storageExampleData struct {
	Cluster string
	Release string
	Nodes   []storageExampleNode
	Node    storageExampleNode
}

var storageExampleNodes = []storageExampleNode{
	{Machine: "node-1", Node: "node-01", Address: "192.168.140.21", Roles: []string{"mon", "mgr", "osd"}, Devices: []string{"/dev/vdb"}},
	{Machine: "node-2", Node: "node-02", Address: "192.168.140.22", Roles: []string{"mon", "mgr", "osd"}, Devices: []string{"/dev/vdb"}},
	{Machine: "node-3", Node: "node-03", Address: "192.168.140.23", Roles: []string{"mon", "osd"}, Devices: []string{"/dev/vdb"}},
}

type storageExampleTemplate struct {
	name string
	body string
}

var storageExampleTemplates = []storageExampleTemplate{
	{name: "environment.yaml", body: storageEnvironmentTmpl},
	{name: "secrets.yaml", body: storageSecretsTmpl},
	{name: "clusters/storage/{{.Cluster}}/cluster.yaml", body: storageClusterTmpl},
}

func storageExampleWorkspace(clusterName, release string) ([]scaffold.File, error) {
	data := storageExampleData{Cluster: clusterName, Release: release}
	for _, node := range storageExampleNodes {
		node.Machine = clusterName + "-" + node.Machine
		data.Nodes = append(data.Nodes, node)
	}
	files := make([]scaffold.File, 0, len(storageExampleTemplates)+len(data.Nodes))
	for _, t := range storageExampleTemplates {
		file, err := renderStorageExampleFile(t.name, t.body, data)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	for _, node := range data.Nodes {
		nodeData := data
		nodeData.Node = node
		file, err := renderStorageExampleFile("clusters/storage/{{.Cluster}}/nodes/{{.Node.Machine}}.yaml", storageMachineTmpl, nodeData)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

func renderStorageExampleFile(namePattern, body string, data storageExampleData) (scaffold.File, error) {
	name, err := renderStorageExampleText(namePattern+"-path", namePattern, data)
	if err != nil {
		return scaffold.File{}, fmt.Errorf("render %s path: %w", namePattern, err)
	}
	rendered, err := renderStorageExampleText(namePattern, body, data)
	if err != nil {
		return scaffold.File{}, fmt.Errorf("render %s: %w", namePattern, err)
	}
	return scaffold.File{Name: name, Body: rendered}, nil
}

func renderStorageExampleText(name, body string, data storageExampleData) (string, error) {
	t, err := template.New(name).Parse(body)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
