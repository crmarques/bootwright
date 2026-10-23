package driver

import (
	"fmt"
	"slices"

	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func placementMaterials(placements []co.SecretPlacement, material map[string]secrets.Material) ([]lifecycle.MaterialFile, []byte, error) {
	files := []lifecycle.MaterialFile{}
	sources := map[string]map[string]string{}
	for i, placement := range placements {
		name := placement.Secret
		keys := placement.Keys
		if placement.Produced != nil {
			name = "produced:export:" + placement.Produced.Object
			keys = []co.KeyPart{{Part: string(secrets.ValuePart), Key: placement.ProducedKey}}
		}
		source, ok := material[name]
		if !ok {
			return nil, nil, failure("placement source "+name+" is not bound for placement", "restore its Secret binding or apply the export producer first")
		}
		mapped := []secrets.Part{}
		for _, key := range keys {
			mapped = append(mapped, secrets.Part(key.Part))
		}
		slices.Sort(mapped)
		if !slices.Equal(mapped, source.Parts()) {
			return nil, nil, failure("Secret placement "+placement.Name+" does not map exactly its bound parts", "correct the package part mapping")
		}
		if _, exists := sources[name]; exists {
			continue
		}
		sources[name] = map[string]string{}
		for j, key := range keys {
			variable := fmt.Sprintf("secret-%d-%d", i, j)
			sources[name][key.Part] = variable
			files = append(files, lifecycle.MaterialFile{Name: variable, Part: secrets.Part(key.Part), Secret: name, Variable: variable})
		}
	}
	data, err := co.Canonical(sources)
	return files, data, err
}
