package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeliciousBuding/DiffAudit-Local-API/internal/api"
)

type runtimeConfig struct {
	ServiceRoot      string
	Host             string
	Port             string
	RepoRoot         string
	ExperimentsRoot  string
	JobsRoot         string
	ProjectRoot      string
	RunnersRoot      string
	ExecutionMode    string
	DockerBinary     string
	GPUSchedulerPath string
	GPURequestDoc    string
	GPUAgentPrefix   string
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func parseConfig(args []string) (runtimeConfig, error) {
	flagSet := flag.NewFlagSet("local-api", flag.ContinueOnError)
	flagSet.SetOutput(os.Stdout)

	defaultServiceRoot, _ := os.Getwd()
	host := flagSet.String("host", envOrDefault("DIFFAUDIT_LOCAL_API_HOST", "127.0.0.1"), "listen host")
	port := flagSet.String("port", envOrDefault("DIFFAUDIT_LOCAL_API_PORT", "8765"), "listen port")
	serviceRoot := flagSet.String("service-root", envOrDefault("DIFFAUDIT_LOCAL_API_SERVICE_ROOT", defaultServiceRoot), "local-api service root")
	runnersRoot := flagSet.String(
		"runners-root",
		envOrDefault("DIFFAUDIT_LOCAL_API_RUNNERS_ROOT", filepath.Join(defaultServiceRoot, "runners")),
		"runners root (contains recon-runner/, pia-runner/, gsa-runner/)",
	)
	projectRoot := flagSet.String("project-root", envOrDefault("DIFFAUDIT_LOCAL_API_PROJECT_ROOT", ""), "project root")
	repoRoot := flagSet.String("repo-root", envOrDefault("DIFFAUDIT_LOCAL_API_REPO_ROOT", ""), "research repo root")
	experimentsRoot := flagSet.String(
		"experiments-root",
		envOrDefault("DIFFAUDIT_LOCAL_API_EXPERIMENTS_ROOT", ""),
		"experiments root",
	)
	jobsRoot := flagSet.String(
		"jobs-root",
		envOrDefault("DIFFAUDIT_LOCAL_API_JOBS_ROOT", ""),
		"jobs root",
	)
	executionMode := flagSet.String(
		"execution-mode",
		envOrDefault("DIFFAUDIT_LOCAL_API_EXECUTION_MODE", "local"),
		"execution mode: local or docker",
	)
	dockerBinary := flagSet.String(
		"docker-binary",
		envOrDefault("DIFFAUDIT_LOCAL_API_DOCKER_BINARY", "docker"),
		"docker cli binary used when execution-mode=docker",
	)
	gpuScheduler := flagSet.String(
		"gpu-scheduler",
		envOrDefault("DIFFAUDIT_LOCAL_API_GPU_SCHEDULER", ""),
		"local gpu scheduler executable",
	)
	gpuRequestDoc := flagSet.String(
		"gpu-request-doc",
		envOrDefault("DIFFAUDIT_LOCAL_API_GPU_REQUEST_DOC", ""),
		"gpu request markdown document",
	)
	gpuAgentPrefix := flagSet.String("gpu-agent-prefix", envOrDefault("DIFFAUDIT_LOCAL_API_GPU_AGENT_PREFIX", "local-api"), "gpu request agent prefix")

	if err := flagSet.Parse(args); err != nil {
		return runtimeConfig{}, err
	}

	projectRootValue := cleanPath(*projectRoot)
	repoRootValue := cleanPath(*repoRoot)
	experimentsRootValue := cleanPath(*experimentsRoot)
	if experimentsRootValue == "" && projectRootValue != "" {
		experimentsRootValue = filepath.Join(projectRootValue, "experiments")
	}
	jobsRootValue := cleanPath(*jobsRoot)
	if jobsRootValue == "" && projectRootValue != "" {
		jobsRootValue = filepath.Join(projectRootValue, "workspaces", "local-api", "jobs")
	}

	return runtimeConfig{
		ServiceRoot:      cleanPath(*serviceRoot),
		Host:             *host,
		Port:             *port,
		RepoRoot:         repoRootValue,
		ExperimentsRoot:  experimentsRootValue,
		JobsRoot:         jobsRootValue,
		ProjectRoot:      projectRootValue,
		RunnersRoot:      cleanPath(*runnersRoot),
		ExecutionMode:    strings.TrimSpace(*executionMode),
		DockerBinary:     strings.TrimSpace(*dockerBinary),
		GPUSchedulerPath: cleanPath(*gpuScheduler),
		GPURequestDoc:    cleanPath(*gpuRequestDoc),
		GPUAgentPrefix:   *gpuAgentPrefix,
	}, nil
}

func cleanPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return filepath.Clean(value)
}

func startupLogLines(config runtimeConfig) []string {
	return []string{
		fmt.Sprintf("listen=%s:%s", config.Host, config.Port),
		fmt.Sprintf("service_root=%s", config.ServiceRoot),
		fmt.Sprintf("project_root=%s", config.ProjectRoot),
		fmt.Sprintf("repo_root=%s", config.RepoRoot),
		fmt.Sprintf("experiments_root=%s", config.ExperimentsRoot),
		fmt.Sprintf("jobs_root=%s", config.JobsRoot),
		fmt.Sprintf("execution_mode=%s", config.ExecutionMode),
		fmt.Sprintf("docker_binary=%s", config.DockerBinary),
		fmt.Sprintf("gpu_scheduler=%s", config.GPUSchedulerPath),
		fmt.Sprintf("gpu_request_doc=%s", config.GPURequestDoc),
		fmt.Sprintf("gpu_agent_prefix=%s", config.GPUAgentPrefix),
	}
}

func configureLogger() *slog.Logger {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)
	return logger
}

func main() {
	config, err := parseConfig(os.Args[1:])
	if err != nil {
		os.Exit(2)
	}

	logger := configureLogger()

	server := api.NewServer(api.Config{
		ServiceRoot:      config.ServiceRoot,
		RunnersRoot:      config.RunnersRoot,
		ExperimentsRoot:  config.ExperimentsRoot,
		JobsRoot:         config.JobsRoot,
		ProjectRoot:      config.ProjectRoot,
		RepoRoot:         config.RepoRoot,
		AutoStartJobs:    true,
		ExecutionMode:    config.ExecutionMode,
		DockerBinary:     config.DockerBinary,
		GPUSchedulerPath: config.GPUSchedulerPath,
		GPURequestDoc:    config.GPURequestDoc,
		GPUAgentPrefix:   config.GPUAgentPrefix,
	})

	address := fmt.Sprintf("%s:%s", config.Host, config.Port)
	logger.Info("DiffAudit Local API starting")
	for _, line := range startupLogLines(config) {
		logger.Info("startup", "detail", line)
	}
	logger.Info("HTTP server listening", "address", address)
	if err := http.ListenAndServe(address, server.Handler()); err != nil {
		logger.Error("HTTP server stopped", "error", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
