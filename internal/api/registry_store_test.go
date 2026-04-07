package api

import "testing"

func TestDefaultRegistryStoreProvidesLiveContracts(t *testing.T) {
	store, err := defaultRegistryStore()
	if err != nil {
		t.Fatalf("defaultRegistryStore returned error: %v", err)
	}

	definition, ok := store.ContractByKey("gray-box/pia/cifar10-ddpm")
	if !ok {
		t.Fatal("expected pia contract in registry db")
	}
	if definition.ContractStatus != "live" {
		t.Fatalf("expected pia contract_status live, got %s", definition.ContractStatus)
	}

	job, definition, ok := store.LiveJobDefinition("gsa_runtime_mainline")
	if !ok {
		t.Fatal("expected gsa_runtime_mainline job in registry db")
	}
	if definition.ContractKey != "white-box/gsa/ddpm-cifar10" {
		t.Fatalf("unexpected contract key %s", definition.ContractKey)
	}
	if job.Runner != "gsa_runtime_mainline" {
		t.Fatalf("unexpected runner %s", job.Runner)
	}
}
