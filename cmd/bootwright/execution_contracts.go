package main

import (
	_ "embed"
	"encoding/json"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

//go:embed execution-contracts.json
var executionManifest []byte

var buildExecutionContracts = loadExecutionContracts()

func currentExecutionContract(block reconciliation.Block) (lifecycle.ExecutionContract, bool) {
	contract, ok := buildExecutionContracts[block.Implementation]
	return contract, ok
}

func loadExecutionContracts() map[string]lifecycle.ExecutionContract {
	var contracts map[string]lifecycle.ExecutionContract
	if json.Unmarshal(executionManifest, &contracts) != nil {
		return nil
	}
	return contracts
}
