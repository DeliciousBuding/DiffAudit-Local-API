package profiles

import (
	"strings"
	"testing"
)

func TestBuildReconArtifactSpecForLocalRuntime(t *testing.T) {
	spec, err := BuildSpec(JobRequest{
		JobType:       "recon_artifact_mainline",
		RuntimeTarget: RuntimeTargetLocal,
		ServiceRoot:   "D:/Code/DiffAudit/Services/Local-API",
		ProjectRoot:   "D:/Code/DiffAudit/Project",
		RepoRoot:      "D:/Code/DiffAudit/Project/external/Reconstruction-based-Attack",
		WorkspacePath: "D:/Code/DiffAudit/Project/experiments/api-job-001",
		Inputs: map[string]any{
			"artifact_dir": "D:/artifacts/recon-scores",
			"method":       "threshold",
		},
	})
	if err != nil {
		t.Fatalf("BuildSpec returned error: %v", err)
	}
	commandLine := strings.Join(spec.Command, "\n")
	if !strings.Contains(commandLine, "run-recon-artifact-mainline") {
		t.Fatalf("expected recon artifact command, got %v", spec.Command)
	}
	if spec.Image != "" {
		t.Fatalf("expected local runtime to avoid docker image, got %s", spec.Image)
	}
	if spec.WorkDir != "D:/Code/DiffAudit/Services/Local-API" {
		t.Fatalf("expected local workdir to be service root, got %s", spec.WorkDir)
	}
	if !strings.Contains(commandLine, "recon-runner") {
		t.Fatalf("expected local command to use recon runner script, got %v", spec.Command)
	}
}

func TestBuildPiaRuntimeMainlineSpecForDockerRuntime(t *testing.T) {
	spec, err := BuildSpec(JobRequest{
		JobType:       "pia_runtime_mainline",
		RuntimeTarget: RuntimeTargetDocker,
		ServiceRoot:   "D:/Code/DiffAudit/Services/Local-API",
		ProjectRoot:   "D:/Code/DiffAudit/Project",
		RepoRoot:      "D:/Code/DiffAudit/Project/external/PIA",
		WorkspacePath: "D:/Code/DiffAudit/Project/workspaces/local-api/jobs/job-001/output",
		Inputs: map[string]any{
			"config":            "D:/Code/DiffAudit/Project/tmp/configs/pia-cifar10-graybox-assets.local.yaml",
			"member_split_root": "D:/Code/DiffAudit/Project/external/PIA/DDPM",
			"device":            "cpu",
			"num_samples":       "16",
		},
	})
	if err != nil {
		t.Fatalf("BuildSpec returned error: %v", err)
	}
	commandLine := strings.Join(spec.Command, "\n")
	if !strings.Contains(commandLine, "run-pia-runtime-mainline") {
		t.Fatalf("expected pia runtime command, got %v", spec.Command)
	}
	if !strings.Contains(commandLine, "--max-samples\n16") {
		t.Fatalf("expected pia spec to pass num_samples as max-samples, got %v", spec.Command)
	}
	if spec.Image == "" {
		t.Fatal("expected docker runtime to select an image")
	}
	if len(spec.Mounts) == 0 {
		t.Fatal("expected docker runtime spec to include mounts")
	}
	for _, mount := range spec.Mounts {
		if strings.Contains(mount.Target, "/workspace/project") {
			t.Fatalf("did not expect docker runtime to mount project source tree, got %+v", mount)
		}
	}
}

func TestBuildGsaRuntimeMainlineSpecForDockerRuntime(t *testing.T) {
	spec, err := BuildSpec(JobRequest{
		JobType:       "gsa_runtime_mainline",
		RuntimeTarget: RuntimeTargetDocker,
		ServiceRoot:   "D:/Code/DiffAudit/Services/Local-API",
		ProjectRoot:   "D:/Code/DiffAudit/Project",
		RepoRoot:      "D:/Code/DiffAudit/Project/workspaces/white-box/external/GSA",
		WorkspacePath: "D:/Code/DiffAudit/Project/workspaces/local-api/jobs/job-002/output",
		Inputs: map[string]any{
			"assets_root":        "D:/Code/DiffAudit/Project/workspaces/white-box/assets/gsa",
			"resolution":         "32",
			"ddpm_num_steps":     "20",
			"sampling_frequency": "2",
			"attack_method":      "1",
		},
	})
	if err != nil {
		t.Fatalf("BuildSpec returned error: %v", err)
	}
	commandLine := strings.Join(spec.Command, "\n")
	for _, want := range []string{
		"run-gsa-runtime-mainline",
		"--assets-root",
		"--ddpm-num-steps\n20",
		"--sampling-frequency\n2",
	} {
		if !strings.Contains(commandLine, want) {
			t.Fatalf("expected gsa command to contain %q, got %v", want, spec.Command)
		}
	}
	if strings.Contains(commandLine, "python\n-m\ndiffaudit") {
		t.Fatalf("expected docker runtime to rely on runner entrypoint instead of project module, got %v", spec.Command)
	}
}
