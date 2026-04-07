package api

import (
	"path/filepath"
	"strings"
	"testing"
)

func containsString(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

func TestBuildExecutionSpecResolvesAssetRefLocal(t *testing.T) {
	root := t.TempDir()
	serviceRoot := filepath.Join(root, "service")
	registryDBPath := filepath.Join(serviceRoot, "config", "registry.db")
	projectRoot := filepath.Join(root, "project")
	repoRoot := filepath.Join(projectRoot, "repo")
	experimentsRoot := filepath.Join(root, "experiments")
	jobsRoot := filepath.Join(root, "jobs")

	server := NewServer(Config{
		ServiceRoot:     serviceRoot,
		RegistryDBPath:  registryDBPath,
		ExperimentsRoot: experimentsRoot,
		JobsRoot:        jobsRoot,
		ProjectRoot:     projectRoot,
		RepoRoot:        repoRoot,
	})
	t.Cleanup(func() {
		if server.registry != nil && server.registry.db != nil {
			_ = server.registry.db.Close()
		}
	})

	assetKey := "gsa.ddpm-cifar10.assets_root"
	if err := server.registry.UpsertAsset(assetKey, "project_root", "workspaces/white-box/assets/gsa", "test asset"); err != nil {
		t.Fatalf("UpsertAsset failed: %v", err)
	}

	payload := auditJobCreate{
		JobType:        "gsa_runtime_mainline",
		ContractKey:    "white-box/gsa/ddpm-cifar10",
		WorkspaceName:  "ws1",
		RuntimeProfile: "local-default",
		JobInputs: map[string]any{
			"assets_root": "asset://" + assetKey,
		},
	}

	spec, err := server.buildExecutionSpec(payload, filepath.Join(experimentsRoot, payload.WorkspaceName), projectRoot, repoRoot)
	if err != nil {
		t.Fatalf("buildExecutionSpec returned error: %v", err)
	}

	want := filepath.Clean(filepath.Join(projectRoot, filepath.FromSlash("workspaces/white-box/assets/gsa")))
	if !containsString(spec.Command, want) {
		t.Fatalf("expected command to contain resolved assets_root %q, got %v", want, spec.Command)
	}
}

func TestBuildExecutionSpecResolvesAssetRefDocker(t *testing.T) {
	root := t.TempDir()
	serviceRoot := filepath.Join(root, "service")
	registryDBPath := filepath.Join(serviceRoot, "config", "registry.db")
	projectRoot := filepath.Join(root, "project")
	repoRoot := filepath.Join(projectRoot, "repo")
	experimentsRoot := filepath.Join(root, "experiments")
	jobsRoot := filepath.Join(root, "jobs")

	server := NewServer(Config{
		ServiceRoot:     serviceRoot,
		RegistryDBPath:  registryDBPath,
		ExperimentsRoot: experimentsRoot,
		JobsRoot:        jobsRoot,
		ProjectRoot:     projectRoot,
		RepoRoot:        repoRoot,
	})
	t.Cleanup(func() {
		if server.registry != nil && server.registry.db != nil {
			_ = server.registry.db.Close()
		}
	})

	assetKey := "gsa.ddpm-cifar10.assets_root"
	if err := server.registry.UpsertAsset(assetKey, "project_root", "workspaces/white-box/assets/gsa", "test asset"); err != nil {
		t.Fatalf("UpsertAsset failed: %v", err)
	}

	payload := auditJobCreate{
		JobType:        "gsa_runtime_mainline",
		ContractKey:    "white-box/gsa/ddpm-cifar10",
		WorkspaceName:  "ws1",
		RuntimeProfile: "docker-default",
		JobInputs: map[string]any{
			"assets_root": "asset://" + assetKey,
		},
	}

	spec, err := server.buildExecutionSpec(payload, filepath.Join(experimentsRoot, payload.WorkspaceName), projectRoot, repoRoot)
	if err != nil {
		t.Fatalf("buildExecutionSpec returned error: %v", err)
	}

	want := "/workspace/project/workspaces/white-box/assets/gsa"
	if !containsString(spec.Command, want) {
		t.Fatalf("expected docker command to contain %q, got %v", want, spec.Command)
	}
}

func TestBuildExecutionSpecErrorsOnUnknownAssetRef(t *testing.T) {
	root := t.TempDir()
	serviceRoot := filepath.Join(root, "service")
	registryDBPath := filepath.Join(serviceRoot, "config", "registry.db")
	projectRoot := filepath.Join(root, "project")
	repoRoot := filepath.Join(projectRoot, "repo")
	experimentsRoot := filepath.Join(root, "experiments")
	jobsRoot := filepath.Join(root, "jobs")

	server := NewServer(Config{
		ServiceRoot:     serviceRoot,
		RegistryDBPath:  registryDBPath,
		ExperimentsRoot: experimentsRoot,
		JobsRoot:        jobsRoot,
		ProjectRoot:     projectRoot,
		RepoRoot:        repoRoot,
	})
	t.Cleanup(func() {
		if server.registry != nil && server.registry.db != nil {
			_ = server.registry.db.Close()
		}
	})

	payload := auditJobCreate{
		JobType:        "gsa_runtime_mainline",
		ContractKey:    "white-box/gsa/ddpm-cifar10",
		WorkspaceName:  "ws1",
		RuntimeProfile: "local-default",
		JobInputs: map[string]any{
			"assets_root": "asset://missing.asset",
		},
	}

	_, err := server.buildExecutionSpec(payload, filepath.Join(experimentsRoot, payload.WorkspaceName), projectRoot, repoRoot)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown asset") {
		t.Fatalf("expected unknown asset error, got %v", err)
	}
}
