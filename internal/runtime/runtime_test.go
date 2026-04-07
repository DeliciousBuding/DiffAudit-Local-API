package runtime

import (
	"strings"
	"testing"
)

func TestDockerExecutorBuildsCommand(t *testing.T) {
	executor := DockerExecutor{Binary: "docker"}
	spec := ExecutionSpec{
		Image:   "diffaudit/pia-runner:latest",
		WorkDir: "/workspace/project",
		Command: []string{"python", "-m", "diffaudit", "run-pia-runtime-mainline"},
		Env: map[string]string{
			"PYTHONUNBUFFERED": "1",
		},
		Mounts: []Mount{
			{Source: "D:/project", Target: "/workspace/project", ReadOnly: true},
			{Source: "D:/jobs/job-001/output", Target: "/job/output", ReadOnly: false},
		},
	}

	command, err := executor.BuildCommand(spec)
	if err != nil {
		t.Fatalf("BuildCommand returned error: %v", err)
	}

	commandLine := strings.Join(command, "\n")
	for _, want := range []string{
		"docker",
		"run",
		"--rm",
		"-w\n/workspace/project",
		"-v\nD:/project:/workspace/project:ro",
		"-v\nD:/jobs/job-001/output:/job/output:rw",
		"-e\nPYTHONUNBUFFERED=1",
		"diffaudit/pia-runner:latest",
		"python\n-m\ndiffaudit\nrun-pia-runtime-mainline",
	} {
		if !strings.Contains(commandLine, want) {
			t.Fatalf("expected docker command to contain %q, got %v", want, command)
		}
	}
}

func TestDockerExecutorRequiresImage(t *testing.T) {
	executor := DockerExecutor{Binary: "docker"}
	_, err := executor.BuildCommand(ExecutionSpec{
		Command: []string{"python"},
	})
	if err == nil {
		t.Fatal("expected missing image error")
	}
}
