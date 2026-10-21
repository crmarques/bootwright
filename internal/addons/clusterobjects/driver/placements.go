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
		source, ok := material[placement.Secret]
		if !ok {
			return nil, nil, failure("Secret "+placement.Secret+" is not bound for placement", "restore its Secret binding")
		}
		mapped := []secrets.Part{}
		for _, key := range placement.Keys {
			mapped = append(mapped, secrets.Part(key.Part))
		}
		slices.Sort(mapped)
		if !slices.Equal(mapped, source.Parts()) {
			return nil, nil, failure("Secret placement "+placement.Name+" does not map exactly its bound parts", "correct the package part mapping")
		}
		if _, exists := sources[placement.Secret]; exists {
			continue
		}
		sources[placement.Secret] = map[string]string{}
		for j, key := range placement.Keys {
			variable := fmt.Sprintf("secret-%d-%d", i, j)
			sources[placement.Secret][key.Part] = variable
			files = append(files, lifecycle.MaterialFile{Name: variable, Part: secrets.Part(key.Part), Secret: placement.Secret, Variable: variable})
		}
	}
	data, err := co.Canonical(sources)
	return files, data, err
}
