package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfigUsesDefaults(t *testing.T) {
	config, err := parseConfig([]string{})
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}
	if config.Host != "127.0.0.1" {
		t.Fatalf("expected default host 127.0.0.1, got %s", config.Host)
	}
	if config.Port != "8765" {
		t.Fatalf("expected default port 8765, got %s", config.Port)
	}
	if config.ProjectRoot != "" {
		t.Fatalf("expected empty default project root, got %s", config.ProjectRoot)
	}
	if config.RepoRoot != "" {
		t.Fatalf("expected empty default repo root, got %s", config.RepoRoot)
	}
	if config.ExperimentsRoot != "" {
		t.Fatalf("expected empty default experiments root, got %s", config.ExperimentsRoot)
	}
	if config.JobsRoot != "" {
		t.Fatalf("expected empty default jobs root, got %s", config.JobsRoot)
	}
	if config.GPUSchedulerPath != "" {
		t.Fatalf("expected empty default gpu scheduler path, got %s", config.GPUSchedulerPath)
	}
	if config.GPURequestDoc != "" {
		t.Fatalf("expected empty default gpu request doc, got %s", config.GPURequestDoc)
	}
}

func TestParseConfigAcceptsOverrides(t *testing.T) {
	config, err := parseConfig([]string{
		"--host", "0.0.0.0",
		"--port", "9001",
		"--project-root", "D:/project",
		"--repo-root", "D:/repo",
		"--experiments-root", "D:/exp",
		"--jobs-root", "D:/jobs",
		"--gpu-scheduler", "D:/gpu-scheduler.exe",
		"--gpu-request-doc", "D:/gpu-resource-requests.md",
		"--gpu-agent-prefix", "api-test",
	})
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}
	if config.Host != "0.0.0.0" {
		t.Fatalf("expected host override, got %s", config.Host)
	}
	if config.Port != "9001" {
		t.Fatalf("expected port override, got %s", config.Port)
	}
	if config.ProjectRoot != filepath.Clean("D:/project") {
		t.Fatalf("expected project-root override, got %s", config.ProjectRoot)
	}
	if config.RepoRoot != filepath.Clean("D:/repo") {
		t.Fatalf("expected repo-root override, got %s", config.RepoRoot)
	}
	if config.ExperimentsRoot != filepath.Clean("D:/exp") {
		t.Fatalf("expected experiments-root override, got %s", config.ExperimentsRoot)
	}
	if config.JobsRoot != filepath.Clean("D:/jobs") {
		t.Fatalf("expected jobs-root override, got %s", config.JobsRoot)
	}
	if config.GPUSchedulerPath != filepath.Clean("D:/gpu-scheduler.exe") {
		t.Fatalf("expected gpu-scheduler override, got %s", config.GPUSchedulerPath)
	}
	if config.GPURequestDoc != filepath.Clean("D:/gpu-resource-requests.md") {
		t.Fatalf("expected gpu-request-doc override, got %s", config.GPURequestDoc)
	}
	if config.GPUAgentPrefix != "api-test" {
		t.Fatalf("expected gpu-agent-prefix override, got %s", config.GPUAgentPrefix)
	}
}

func TestParseConfigReadsEnvironmentDefaults(t *testing.T) {
	t.Setenv("DIFFAUDIT_LOCAL_API_HOST", "0.0.0.0")
	t.Setenv("DIFFAUDIT_LOCAL_API_PORT", "9901")
	t.Setenv("DIFFAUDIT_LOCAL_API_PROJECT_ROOT", "D:/portable/project")
	t.Setenv("DIFFAUDIT_LOCAL_API_REPO_ROOT", "D:/portable/repo")
	t.Setenv("DIFFAUDIT_LOCAL_API_EXPERIMENTS_ROOT", "D:/portable/project/experiments")
	t.Setenv("DIFFAUDIT_LOCAL_API_JOBS_ROOT", "D:/portable/project/jobs")
	t.Setenv("DIFFAUDIT_LOCAL_API_GPU_SCHEDULER", "D:/portable/gpu-scheduler.exe")
	t.Setenv("DIFFAUDIT_LOCAL_API_GPU_REQUEST_DOC", "D:/portable/gpu-resource-requests.md")
	t.Setenv("DIFFAUDIT_LOCAL_API_GPU_AGENT_PREFIX", "portable-api")

	config, err := parseConfig([]string{})
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}
	if config.Host != "0.0.0.0" {
		t.Fatalf("expected env host override, got %s", config.Host)
	}
	if config.Port != "9901" {
		t.Fatalf("expected env port override, got %s", config.Port)
	}
	if config.ProjectRoot != filepath.Clean("D:/portable/project") {
		t.Fatalf("expected env project root, got %s", config.ProjectRoot)
	}
	if config.RepoRoot != filepath.Clean("D:/portable/repo") {
		t.Fatalf("expected env repo root, got %s", config.RepoRoot)
	}
	if config.ExperimentsRoot != filepath.Clean("D:/portable/project/experiments") {
		t.Fatalf("expected env experiments root, got %s", config.ExperimentsRoot)
	}
	if config.JobsRoot != filepath.Clean("D:/portable/project/jobs") {
		t.Fatalf("expected env jobs root, got %s", config.JobsRoot)
	}
	if config.GPUSchedulerPath != filepath.Clean("D:/portable/gpu-scheduler.exe") {
		t.Fatalf("expected env gpu scheduler, got %s", config.GPUSchedulerPath)
	}
	if config.GPURequestDoc != filepath.Clean("D:/portable/gpu-resource-requests.md") {
		t.Fatalf("expected env gpu request doc, got %s", config.GPURequestDoc)
	}
	if config.GPUAgentPrefix != "portable-api" {
		t.Fatalf("expected env gpu agent prefix, got %s", config.GPUAgentPrefix)
	}
}

func TestParseConfigDerivesProjectChildrenOnlyFromExplicitProjectRoot(t *testing.T) {
	config, err := parseConfig([]string{
		"--project-root", `D:\Code\DiffAudit\Project`,
	})
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}

	if config.ProjectRoot != filepath.Clean(`D:\Code\DiffAudit\Project`) {
		t.Fatalf("expected explicit project root, got %s", config.ProjectRoot)
	}
	if config.ExperimentsRoot != filepath.Clean(`D:\Code\DiffAudit\Project\experiments`) {
		t.Fatalf("expected derived experiments root, got %s", config.ExperimentsRoot)
	}
	if config.JobsRoot != filepath.Clean(`D:\Code\DiffAudit\Project\workspaces\local-api\jobs`) {
		t.Fatalf("expected derived jobs root, got %s", config.JobsRoot)
	}
	if config.RepoRoot != "" {
		t.Fatalf("expected repo root to stay explicit, got %s", config.RepoRoot)
	}
	if config.GPUSchedulerPath != "" {
		t.Fatalf("expected gpu scheduler path to stay explicit, got %s", config.GPUSchedulerPath)
	}
	if config.GPURequestDoc != "" {
		t.Fatalf("expected gpu request doc to stay explicit, got %s", config.GPURequestDoc)
	}
}

func TestStartupLogLinesIncludeResolvedPaths(t *testing.T) {
	config := runtimeConfig{
		Host:             "0.0.0.0",
		Port:             "8765",
		ProjectRoot:      `D:\Code\DiffAudit\Project`,
		RepoRoot:         `D:\Code\DiffAudit\Project\external\Reconstruction-based-Attack`,
		ExperimentsRoot:  `D:\Code\DiffAudit\Project\experiments`,
		JobsRoot:         `D:\Code\DiffAudit\Project\workspaces\local-api\jobs`,
		GPUSchedulerPath: `D:\Code\DiffAudit\LocalOps\paper-resource-scheduler\gpu-scheduler.exe`,
		GPURequestDoc:    `D:\Code\DiffAudit\LocalOps\paper-resource-scheduler\gpu-resource-requests.md`,
		GPUAgentPrefix:   "local-api",
	}

	lines := startupLogLines(config)
	joined := strings.Join(lines, "\n")

	for _, want := range []string{
		"listen=0.0.0.0:8765",
		"project_root=D:\\Code\\DiffAudit\\Project",
		"repo_root=D:\\Code\\DiffAudit\\Project\\external\\Reconstruction-based-Attack",
		"experiments_root=D:\\Code\\DiffAudit\\Project\\experiments",
		"jobs_root=D:\\Code\\DiffAudit\\Project\\workspaces\\local-api\\jobs",
		"gpu_scheduler=D:\\Code\\DiffAudit\\LocalOps\\paper-resource-scheduler\\gpu-scheduler.exe",
		"gpu_request_doc=D:\\Code\\DiffAudit\\LocalOps\\paper-resource-scheduler\\gpu-resource-requests.md",
		"gpu_agent_prefix=local-api",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("startup lines missing %q:\n%s", want, joined)
		}
	}
}
