package profiles

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeliciousBuding/DiffAudit-Local-API/internal/runtime"
)

type RuntimeTarget string

const (
	RuntimeTargetLocal  RuntimeTarget = "local"
	RuntimeTargetDocker RuntimeTarget = "docker"
)

type JobRequest struct {
	JobType       string
	RuntimeTarget RuntimeTarget
	ServiceRoot   string
	RunnersRoot   string
	ProjectRoot   string
	RepoRoot      string
	WorkspacePath string
	Inputs        map[string]any
}

const (
	reconImage = "diffaudit/recon-runner:latest"
	piaImage   = "diffaudit/pia-runner:latest"
	gsaImage   = "diffaudit/gsa-runner:latest"
)

func runnerScript(request JobRequest, runnerName string) string {
	root := strings.TrimSpace(request.RunnersRoot)
	if root == "" {
		root = filepath.Join(request.ServiceRoot, "runners")
	}
	return filepath.Join(root, runnerName, "run.py")
}

func BuildSpec(request JobRequest) (runtime.ExecutionSpec, error) {
	switch request.JobType {
	case "recon_artifact_mainline":
		return buildReconArtifactMainlineSpec(request)
	case "recon_runtime_mainline":
		return buildReconRuntimeMainlineSpec(request)
	case "pia_runtime_mainline":
		return buildPiaRuntimeMainlineSpec(request)
	case "gsa_runtime_mainline":
		return buildGsaRuntimeMainlineSpec(request)
	default:
		return runtime.ExecutionSpec{}, errors.New("unsupported job_type")
	}
}

func buildReconArtifactMainlineSpec(request JobRequest) (runtime.ExecutionSpec, error) {
	artifactDir := inputString(request.Inputs, "artifact_dir")
	if artifactDir == "" {
		return runtime.ExecutionSpec{}, errors.New("recon_artifact_mainline requires artifact_dir")
	}
	method := inputOrDefault(request.Inputs, "method", "threshold")
	if request.RuntimeTarget == RuntimeTargetDocker {
		plan := newRunnerDockerPlan(request.WorkspacePath)
		return runtime.ExecutionSpec{
			Image:   reconImage,
			WorkDir: "/runner",
			Command: []string{
				"run-recon-artifact-mainline",
				"--artifact-dir", plan.containerPath("artifact_dir", artifactDir),
				"--workspace", plan.containerWorkspaceRoot,
				"--repo-root", plan.containerPath("repo_root", request.RepoRoot),
				"--method", method,
			},
			Env: map[string]string{
				"PYTHONUNBUFFERED": "1",
			},
			Mounts: plan.mounts(),
		}, nil
	}
	return runtime.ExecutionSpec{
		WorkDir: request.ServiceRoot,
		Command: []string{
			"python", runnerScript(request, "recon-runner"), "run-recon-artifact-mainline",
			"--artifact-dir", artifactDir,
			"--workspace", request.WorkspacePath,
			"--repo-root", request.RepoRoot,
			"--method", method,
		},
		Env: map[string]string{
			"PYTHONUNBUFFERED": "1",
		},
	}, nil
}

func buildReconRuntimeMainlineSpec(request JobRequest) (runtime.ExecutionSpec, error) {
	required := []string{
		"target_member_dataset",
		"target_nonmember_dataset",
		"shadow_member_dataset",
		"shadow_nonmember_dataset",
		"target_model_dir",
		"shadow_model_dir",
	}
	for _, key := range required {
		if inputString(request.Inputs, key) == "" {
			return runtime.ExecutionSpec{}, errors.New("recon_runtime_mainline requires " + key)
		}
	}
	method := inputOrDefault(request.Inputs, "method", "threshold")
	backend := inputOrDefault(request.Inputs, "backend", "stable_diffusion")
	scheduler := inputOrDefault(request.Inputs, "scheduler", "default")
	if request.RuntimeTarget == RuntimeTargetDocker {
		plan := newRunnerDockerPlan(request.WorkspacePath)
		return runtime.ExecutionSpec{
			Image:   reconImage,
			WorkDir: "/runner",
			Command: []string{
				"run-recon-runtime-mainline",
				"--workspace", plan.containerWorkspaceRoot,
				"--repo-root", plan.containerPath("repo_root", request.RepoRoot),
				"--target-member-dataset", plan.containerPath("target_member_dataset", inputString(request.Inputs, "target_member_dataset")),
				"--target-nonmember-dataset", plan.containerPath("target_nonmember_dataset", inputString(request.Inputs, "target_nonmember_dataset")),
				"--shadow-member-dataset", plan.containerPath("shadow_member_dataset", inputString(request.Inputs, "shadow_member_dataset")),
				"--shadow-nonmember-dataset", plan.containerPath("shadow_nonmember_dataset", inputString(request.Inputs, "shadow_nonmember_dataset")),
				"--target-model-dir", plan.containerPath("target_model_dir", inputString(request.Inputs, "target_model_dir")),
				"--shadow-model-dir", plan.containerPath("shadow_model_dir", inputString(request.Inputs, "shadow_model_dir")),
				"--backend", backend,
				"--scheduler", scheduler,
				"--method", method,
			},
			Env: map[string]string{
				"PYTHONUNBUFFERED": "1",
			},
			Mounts: plan.mounts(),
		}, nil
	}
	return runtime.ExecutionSpec{
		WorkDir: request.ServiceRoot,
		Command: []string{
			"python", runnerScript(request, "recon-runner"), "run-recon-runtime-mainline",
			"--workspace", request.WorkspacePath,
			"--repo-root", request.RepoRoot,
			"--target-member-dataset", inputString(request.Inputs, "target_member_dataset"),
			"--target-nonmember-dataset", inputString(request.Inputs, "target_nonmember_dataset"),
			"--shadow-member-dataset", inputString(request.Inputs, "shadow_member_dataset"),
			"--shadow-nonmember-dataset", inputString(request.Inputs, "shadow_nonmember_dataset"),
			"--target-model-dir", inputString(request.Inputs, "target_model_dir"),
			"--shadow-model-dir", inputString(request.Inputs, "shadow_model_dir"),
			"--backend", backend,
			"--scheduler", scheduler,
			"--method", method,
		},
		Env: map[string]string{
			"PYTHONUNBUFFERED": "1",
		},
	}, nil
}

func buildPiaRuntimeMainlineSpec(request JobRequest) (runtime.ExecutionSpec, error) {
	configPath := inputString(request.Inputs, "config")
	if configPath == "" {
		return runtime.ExecutionSpec{}, errors.New("pia_runtime_mainline requires config")
	}
	memberSplitRoot := inputOrDefault(request.Inputs, "member_split_root", filepath.Join(request.ProjectRoot, "external", "PIA", "DDPM"))
	device := inputOrDefault(request.Inputs, "device", "cpu")
	provenanceStatus := inputOrDefault(request.Inputs, "provenance_status", "source-retained-unverified")
	command := []string{
		"python", runnerScript(request, "pia-runner"), "run-pia-runtime-mainline",
		"--config", configPath,
		"--workspace", request.WorkspacePath,
		"--repo-root", request.RepoRoot,
		"--member-split-root", memberSplitRoot,
		"--device", device,
		"--provenance-status", provenanceStatus,
	}
	if maxSamples := inputString(request.Inputs, "num_samples"); maxSamples != "" {
		command = append(command, "--max-samples", maxSamples)
	}
	if batchSize := inputString(request.Inputs, "batch_size"); batchSize != "" {
		command = append(command, "--batch-size", batchSize)
	}
	if inputBool(request.Inputs, "stochastic_dropout_defense") {
		command = append(command, "--stochastic-dropout-defense")
	}
	if request.RuntimeTarget == RuntimeTargetDocker {
		plan := newRunnerDockerPlan(request.WorkspacePath)
		dockerCommand := []string{
			"run-pia-runtime-mainline",
			"--config", plan.containerPath("config", configPath),
			"--workspace", plan.containerWorkspaceRoot,
			"--repo-root", plan.containerPath("repo_root", request.RepoRoot),
			"--member-split-root", plan.containerPath("member_split_root", memberSplitRoot),
			"--device", device,
			"--provenance-status", provenanceStatus,
		}
		if maxSamples := inputString(request.Inputs, "num_samples"); maxSamples != "" {
			dockerCommand = append(dockerCommand, "--max-samples", maxSamples)
		}
		if batchSize := inputString(request.Inputs, "batch_size"); batchSize != "" {
			dockerCommand = append(dockerCommand, "--batch-size", batchSize)
		}
		if inputBool(request.Inputs, "stochastic_dropout_defense") {
			dockerCommand = append(dockerCommand, "--stochastic-dropout-defense")
		}
		return runtime.ExecutionSpec{
			Image:   piaImage,
			WorkDir: "/runner",
			Command: dockerCommand,
			Env: map[string]string{
				"PYTHONUNBUFFERED": "1",
			},
			Mounts: plan.mounts(),
		}, nil
	}
	return runtime.ExecutionSpec{
		WorkDir: request.ServiceRoot,
		Command: command,
		Env: map[string]string{
			"PYTHONUNBUFFERED": "1",
		},
	}, nil
}

func buildGsaRuntimeMainlineSpec(request JobRequest) (runtime.ExecutionSpec, error) {
	assetsRoot := inputString(request.Inputs, "assets_root")
	if assetsRoot == "" {
		return runtime.ExecutionSpec{}, errors.New("gsa_runtime_mainline requires assets_root")
	}
	command := []string{
		"python", runnerScript(request, "gsa-runner"), "run-gsa-runtime-mainline",
		"--workspace", request.WorkspacePath,
		"--repo-root", request.RepoRoot,
		"--assets-root", assetsRoot,
		"--resolution", inputOrDefault(request.Inputs, "resolution", "32"),
		"--ddpm-num-steps", inputOrDefault(request.Inputs, "ddpm_num_steps", "20"),
		"--sampling-frequency", inputOrDefault(request.Inputs, "sampling_frequency", "2"),
		"--attack-method", inputOrDefault(request.Inputs, "attack_method", "1"),
		"--prediction-type", inputOrDefault(request.Inputs, "prediction_type", "epsilon"),
		"--provenance-status", inputOrDefault(request.Inputs, "provenance_status", "workspace-verified"),
	}
	if request.RuntimeTarget == RuntimeTargetDocker {
		plan := newRunnerDockerPlan(request.WorkspacePath)
		return runtime.ExecutionSpec{
			Image:   gsaImage,
			WorkDir: "/runner",
			Command: []string{
				"run-gsa-runtime-mainline",
				"--workspace", plan.containerWorkspaceRoot,
				"--repo-root", plan.containerPath("repo_root", request.RepoRoot),
				"--assets-root", plan.containerPath("assets_root", assetsRoot),
				"--resolution", inputOrDefault(request.Inputs, "resolution", "32"),
				"--ddpm-num-steps", inputOrDefault(request.Inputs, "ddpm_num_steps", "20"),
				"--sampling-frequency", inputOrDefault(request.Inputs, "sampling_frequency", "2"),
				"--attack-method", inputOrDefault(request.Inputs, "attack_method", "1"),
				"--prediction-type", inputOrDefault(request.Inputs, "prediction_type", "epsilon"),
				"--provenance-status", inputOrDefault(request.Inputs, "provenance_status", "workspace-verified"),
			},
			Env: map[string]string{
				"PYTHONUNBUFFERED": "1",
			},
			Mounts: plan.mounts(),
		}, nil
	}
	return runtime.ExecutionSpec{
		WorkDir: request.ServiceRoot,
		Command: command,
		Env: map[string]string{
			"PYTHONUNBUFFERED": "1",
		},
	}, nil
}

func inputString(inputs map[string]any, key string) string {
	if inputs == nil {
		return ""
	}
	if value, ok := inputs[key]; ok {
		switch typed := value.(type) {
		case string:
			return strings.TrimSpace(typed)
		}
	}
	return ""
}

func inputOrDefault(inputs map[string]any, key string, fallback string) string {
	if value := inputString(inputs, key); value != "" {
		return value
	}
	return fallback
}

func inputBool(inputs map[string]any, key string) bool {
	if inputs == nil {
		return false
	}
	value, ok := inputs[key]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		lowered := strings.ToLower(strings.TrimSpace(typed))
		return lowered == "1" || lowered == "true" || lowered == "yes"
	default:
		return false
	}
}

type dockerPlan struct {
	workspacePath          string
	containerWorkspaceRoot string
	mountMap               map[string]runtime.Mount
}

func newRunnerDockerPlan(workspacePath string) *dockerPlan {
	plan := &dockerPlan{
		workspacePath:          filepath.Clean(workspacePath),
		containerWorkspaceRoot: "/job/output",
		mountMap:               map[string]runtime.Mount{},
	}
	plan.mountMap["workspace-root"] = runtime.Mount{
		Source:   plan.workspacePath,
		Target:   plan.containerWorkspaceRoot,
		ReadOnly: false,
	}
	return plan
}

func (p *dockerPlan) containerPath(label string, hostPath string) string {
	cleaned := filepath.Clean(hostPath)
	if cleaned == p.workspacePath {
		return p.containerWorkspaceRoot
	}
	target := "/job/inputs/" + sanitizeLabel(label)
	p.mountMap[label] = runtime.Mount{
		Source:   cleaned,
		Target:   target,
		ReadOnly: true,
	}
	return filepath.ToSlash(target)
}

func (p *dockerPlan) mounts() []runtime.Mount {
	keys := make([]string, 0, len(p.mountMap))
	for key := range p.mountMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	mounts := make([]runtime.Mount, 0, len(keys))
	for _, key := range keys {
		mounts = append(mounts, p.mountMap[key])
	}
	return mounts
}

func sanitizeLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "-")
	return value
}
