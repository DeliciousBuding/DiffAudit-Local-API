package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"diffaudit/local-api-go/internal/api"
)

type runtimeConfig struct {
	Host             string
	Port             string
	ExperimentsRoot  string
	JobsRoot         string
	ProjectRoot      string
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

	projectRootFallback := detectProjectRoot(defaultPath("."))
	projectRootDefault := envOrDefault("DIFFAUDIT_LOCAL_API_PROJECT_ROOT", projectRootFallback)
	localOpsRootFallback := detectLocalOpsRoot(projectRootDefault)
	host := flagSet.String("host", envOrDefault("DIFFAUDIT_LOCAL_API_HOST", "127.0.0.1"), "listen host")
	port := flagSet.String("port", envOrDefault("DIFFAUDIT_LOCAL_API_PORT", "8765"), "listen port")
	experimentsRoot := flagSet.String(
		"experiments-root",
		envOrDefault("DIFFAUDIT_LOCAL_API_EXPERIMENTS_ROOT", filepath.Join(projectRootDefault, "experiments")),
		"experiments root",
	)
	jobsRoot := flagSet.String(
		"jobs-root",
		envOrDefault("DIFFAUDIT_LOCAL_API_JOBS_ROOT", filepath.Join(projectRootDefault, "workspaces", "local-api", "jobs")),
		"jobs root",
	)
	projectRoot := flagSet.String("project-root", projectRootDefault, "project root")
	gpuScheduler := flagSet.String(
		"gpu-scheduler",
		envOrDefault("DIFFAUDIT_LOCAL_API_GPU_SCHEDULER", filepath.Join(localOpsRootFallback, "paper-resource-scheduler", "gpu-scheduler.exe")),
		"local gpu scheduler executable",
	)
	gpuRequestDoc := flagSet.String(
		"gpu-request-doc",
		envOrDefault("DIFFAUDIT_LOCAL_API_GPU_REQUEST_DOC", filepath.Join(localOpsRootFallback, "paper-resource-scheduler", "gpu-resource-requests.md")),
		"gpu request markdown document",
	)
	gpuAgentPrefix := flagSet.String("gpu-agent-prefix", envOrDefault("DIFFAUDIT_LOCAL_API_GPU_AGENT_PREFIX", "local-api"), "gpu request agent prefix")

	if err := flagSet.Parse(args); err != nil {
		return runtimeConfig{}, err
	}

	return runtimeConfig{
		Host:             *host,
		Port:             *port,
		ExperimentsRoot:  *experimentsRoot,
		JobsRoot:         *jobsRoot,
		ProjectRoot:      *projectRoot,
		GPUSchedulerPath: *gpuScheduler,
		GPURequestDoc:    *gpuRequestDoc,
		GPUAgentPrefix:   *gpuAgentPrefix,
	}, nil
}

func defaultPath(relative string) string {
	current, err := os.Getwd()
	if err != nil {
		return relative
	}
	if relative == "." {
		return current
	}
	return filepath.Clean(filepath.Join(current, relative))
}

func detectProjectRoot(current string) string {
	cleaned := filepath.Clean(current)
	servicePath := filepath.Join("Services", "Local-API")
	if strings.HasSuffix(cleaned, servicePath) {
		return filepath.Clean(filepath.Join(cleaned, "..", "..", "Project"))
	}
	return cleaned
}

func detectLocalOpsRoot(projectRoot string) string {
	return filepath.Clean(filepath.Join(projectRoot, "..", "LocalOps"))
}

func startupLogLines(config runtimeConfig) []string {
	return []string{
		fmt.Sprintf("listen=%s:%s", config.Host, config.Port),
		fmt.Sprintf("project_root=%s", config.ProjectRoot),
		fmt.Sprintf("experiments_root=%s", config.ExperimentsRoot),
		fmt.Sprintf("jobs_root=%s", config.JobsRoot),
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
		ExperimentsRoot:  config.ExperimentsRoot,
		JobsRoot:         config.JobsRoot,
		ProjectRoot:      config.ProjectRoot,
		AutoStartJobs:    true,
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
