package main

import (
	"path/filepath"
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
}

func TestParseConfigAcceptsOverrides(t *testing.T) {
	config, err := parseConfig([]string{
		"--host", "0.0.0.0",
		"--port", "9001",
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
	if config.ExperimentsRoot != "D:/exp" {
		t.Fatalf("expected experiments-root override, got %s", config.ExperimentsRoot)
	}
	if config.JobsRoot != "D:/jobs" {
		t.Fatalf("expected jobs-root override, got %s", config.JobsRoot)
	}
	if config.GPUSchedulerPath != "D:/gpu-scheduler.exe" {
		t.Fatalf("expected gpu-scheduler override, got %s", config.GPUSchedulerPath)
	}
	if config.GPURequestDoc != "D:/gpu-resource-requests.md" {
		t.Fatalf("expected gpu-request-doc override, got %s", config.GPURequestDoc)
	}
	if config.GPUAgentPrefix != "api-test" {
		t.Fatalf("expected gpu-agent-prefix override, got %s", config.GPUAgentPrefix)
	}
}

func TestDetectProjectRootFromToolDirectory(t *testing.T) {
	input := filepath.Clean(`D:\Code\DiffAudit\Services\Local-API`)
	expected := filepath.Clean(`D:\Code\DiffAudit\Project`)

	actual := detectProjectRoot(input)

	if actual != expected {
		t.Fatalf("expected %s, got %s", expected, actual)
	}
}

func TestDetectLocalOpsRootFromProjectRoot(t *testing.T) {
	projectRoot := filepath.Clean(`D:\Code\DiffAudit\Project`)
	expected := filepath.Clean(`D:\Code\DiffAudit\LocalOps`)

	actual := detectLocalOpsRoot(projectRoot)

	if actual != expected {
		t.Fatalf("expected %s, got %s", expected, actual)
	}
}
