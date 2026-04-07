package runtime

import (
	"errors"
	"os/exec"
	"sort"
)

type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

type ExecutionSpec struct {
	Image   string
	WorkDir string
	Command []string
	Env     map[string]string
	Mounts  []Mount
}

type CommandExecutor func(command []string, dir string) ([]byte, error)

type Executor interface {
	BuildCommand(spec ExecutionSpec) ([]string, error)
	Execute(spec ExecutionSpec) ([]byte, error)
}

type LocalExecutor struct {
	ExecCommand CommandExecutor
}

type DockerExecutor struct {
	Binary      string
	ExecCommand CommandExecutor
}

func (e LocalExecutor) BuildCommand(spec ExecutionSpec) ([]string, error) {
	if len(spec.Command) == 0 {
		return nil, errors.New("command is required")
	}
	return append([]string(nil), spec.Command...), nil
}

func (e LocalExecutor) Execute(spec ExecutionSpec) ([]byte, error) {
	command, err := e.BuildCommand(spec)
	if err != nil {
		return nil, err
	}
	execFn := e.ExecCommand
	if execFn == nil {
		execFn = defaultExecCommand
	}
	return execFn(command, spec.WorkDir)
}

func (e DockerExecutor) BuildCommand(spec ExecutionSpec) ([]string, error) {
	if len(spec.Command) == 0 {
		return nil, errors.New("command is required")
	}
	if spec.Image == "" {
		return nil, errors.New("docker execution requires image")
	}
	binary := e.Binary
	if binary == "" {
		binary = "docker"
	}
	command := []string{binary, "run", "--rm"}
	if spec.WorkDir != "" {
		command = append(command, "-w", spec.WorkDir)
	}
	if len(spec.Env) > 0 {
		keys := make([]string, 0, len(spec.Env))
		for key := range spec.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			command = append(command, "-e", key+"="+spec.Env[key])
		}
	}
	for _, mount := range spec.Mounts {
		mode := "rw"
		if mount.ReadOnly {
			mode = "ro"
		}
		command = append(command, "-v", mount.Source+":"+mount.Target+":"+mode)
	}
	command = append(command, spec.Image)
	command = append(command, spec.Command...)
	return command, nil
}

func (e DockerExecutor) Execute(spec ExecutionSpec) ([]byte, error) {
	command, err := e.BuildCommand(spec)
	if err != nil {
		return nil, err
	}
	execFn := e.ExecCommand
	if execFn == nil {
		execFn = defaultExecCommand
	}
	return execFn(command, "")
}

func defaultExecCommand(command []string, dir string) ([]byte, error) {
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}
