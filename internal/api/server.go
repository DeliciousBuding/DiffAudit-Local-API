package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Config struct {
	ExperimentsRoot  string
	JobsRoot         string
	ProjectRoot      string
	RepoRoot         string
	AutoStartJobs    bool
	Executor         func(payload auditJobCreate, workspacePath string) error
	ExecCommand      func(command []string, dir string) ([]byte, error)
	GPUSchedulerPath string
	GPURequestDoc    string
	GPUAgentPrefix   string
	AcquireGPU       func(agent string) (func(), error)
}

type Server struct {
	config Config
	mux    *http.ServeMux
}

type auditJobCreate struct {
	JobType       string `json:"job_type"`
	WorkspaceName string `json:"workspace_name"`
	RepoRoot      string `json:"repo_root,omitempty"`
	Method        string `json:"method,omitempty"`
	ArtifactDir   string `json:"artifact_dir,omitempty"`
}

type auditJobRecord struct {
	JobID         string         `json:"job_id"`
	JobType       string         `json:"job_type"`
	Status        string         `json:"status"`
	WorkspaceName string         `json:"workspace_name"`
	CreatedAt     string         `json:"created_at"`
	UpdatedAt     string         `json:"updated_at"`
	Payload       auditJobCreate `json:"payload"`
	Command       []string       `json:"command"`
	SummaryPath   *string        `json:"summary_path"`
	Metrics       any            `json:"metrics"`
	Error         *string        `json:"error"`
	StdoutTail    []string       `json:"stdout_tail"`
	StderrTail    []string       `json:"stderr_tail"`
}

type modelOption struct {
	Key          string  `json:"key"`
	Label        string  `json:"label"`
	AccessLevel  string  `json:"access_level"`
	Availability string  `json:"availability"`
	Paper        string  `json:"paper"`
	Method       string  `json:"method"`
	Backend      string  `json:"backend"`
	Scheduler    *string `json:"scheduler"`
}

type catalogEntry struct {
	Key             string  `json:"key"`
	AccessLevel     string  `json:"access_level"`
	AttackFamily    string  `json:"attack_family"`
	TargetKey       string  `json:"target_key"`
	Availability    string  `json:"availability"`
	EvidenceLevel   string  `json:"evidence_level"`
	Label           string  `json:"label"`
	Paper           string  `json:"paper"`
	Backend         string  `json:"backend"`
	Scheduler       *string `json:"scheduler"`
	BestSummaryPath *string `json:"best_summary_path"`
	BestWorkspace   *string `json:"best_workspace"`
}

type catalogEvidence struct {
	summaryPath string
	workspace   string
}

type configError struct {
	message string
}

func (err configError) Error() string {
	return err.message
}

var models = []modelOption{
	{
		Key:          "sd15-ddim",
		Label:        "Stable Diffusion 1.5 + DDIM",
		AccessLevel:  "black-box",
		Availability: "ready",
		Paper:        "BlackBox_Reconstruction_ArXiv2023",
		Method:       "recon",
		Backend:      "stable_diffusion",
		Scheduler:    stringPtr("ddim"),
	},
	{
		Key:          "kandinsky-v22",
		Label:        "Kandinsky v2.2",
		AccessLevel:  "black-box",
		Availability: "partial",
		Paper:        "BlackBox_Reconstruction_ArXiv2023",
		Method:       "recon",
		Backend:      "kandinsky_v22",
	},
	{
		Key:          "dit-xl2-256",
		Label:        "DiT-XL/2 256",
		AccessLevel:  "black-box",
		Availability: "partial",
		Paper:        "Scalable_Diffusion_Transformers_2022",
		Method:       "sample",
		Backend:      "dit",
	},
}

func stringPtr(value string) *string {
	return &value
}

func NewServer(config Config) *Server {
	if config.GPUAgentPrefix == "" {
		config.GPUAgentPrefix = "local-api"
	}
	mux := http.NewServeMux()
	server := &Server{
		config: config,
		mux:    mux,
	}
	mux.HandleFunc("GET /health", server.handleHealth)
	mux.HandleFunc("GET /api/v1/catalog", server.handleCatalog)
	mux.HandleFunc("GET /api/v1/models", server.handleModels)
	mux.HandleFunc("GET /api/v1/experiments/recon/best", server.handleBestRecon)
	mux.HandleFunc("GET /api/v1/experiments/{workspace}/summary", server.handleWorkspaceSummary)
	mux.HandleFunc("GET /api/v1/audit/jobs", server.handleListJobs)
	mux.HandleFunc("POST /api/v1/audit/jobs", server.handleCreateJob)
	mux.HandleFunc("GET /api/v1/audit/jobs/{jobID}", server.handleGetJob)
	return server
}

func (s *Server) Handler() http.Handler {
	return requestLoggingMiddleware(slog.Default(), s.mux)
}

type statusCapturingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (writer *statusCapturingResponseWriter) WriteHeader(statusCode int) {
	writer.statusCode = statusCode
	writer.ResponseWriter.WriteHeader(statusCode)
}

func requestLoggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		captured := &statusCapturingResponseWriter{
			ResponseWriter: writer,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(captured, request)

		logger.Info(
			"request completed",
			"method", request.Method,
			"path", request.URL.Path,
			"status", captured.statusCode,
			"remote_addr", request.RemoteAddr,
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
	})
}

func (s *Server) handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{
		"status":           "ok",
		"experiments_root": s.config.ExperimentsRoot,
		"jobs_root":        s.config.JobsRoot,
	})
}

func (s *Server) handleModels(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, models)
}

func (s *Server) handleCatalog(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, s.catalogEntries())
}

func (s *Server) handleBestRecon(writer http.ResponseWriter, _ *http.Request) {
	summaryPath, err := s.bestReconSummaryPath()
	if err != nil {
		writeError(writer, statusCodeForError(err, http.StatusNotFound), err.Error())
		return
	}
	s.handleSummaryPath(writer, summaryPath)
}

func (s *Server) handleWorkspaceSummary(writer http.ResponseWriter, request *http.Request) {
	workspace := request.PathValue("workspace")
	if workspace == "" {
		writeError(writer, http.StatusBadRequest, "workspace is required")
		return
	}
	experimentsRoot, err := requireConfigPath("experiments_root", s.config.ExperimentsRoot, "read experiment summaries")
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, err.Error())
		return
	}
	summaryPath := filepath.Join(experimentsRoot, workspace, "summary.json")
	s.handleSummaryPath(writer, summaryPath)
}

func (s *Server) handleListJobs(writer http.ResponseWriter, _ *http.Request) {
	jobs, err := s.listJobs()
	if err != nil {
		writeError(writer, statusCodeForError(err, http.StatusInternalServerError), err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, jobs)
}

func (s *Server) handleCreateJob(writer http.ResponseWriter, request *http.Request) {
	var payload auditJobCreate
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := validateCreatePayload(payload); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	hasActive, err := s.hasActiveWorkspaceJob(payload.WorkspaceName)
	if err != nil {
		writeError(writer, statusCodeForError(err, http.StatusInternalServerError), err.Error())
		return
	}
	if hasActive {
		writeError(writer, http.StatusConflict, "workspace '"+payload.WorkspaceName+"' already has an active job")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	record := auditJobRecord{
		JobID:         "job_" + strings.ReplaceAll(now, ":", "") + "_" + payload.WorkspaceName,
		JobType:       payload.JobType,
		Status:        "queued",
		WorkspaceName: payload.WorkspaceName,
		CreatedAt:     now,
		UpdatedAt:     now,
		Payload:       payload,
		Command:       nil,
		SummaryPath:   nil,
		Metrics:       nil,
		Error:         nil,
		StdoutTail:    nil,
		StderrTail:    nil,
	}
	if err := s.writeJob(record); err != nil {
		writeError(writer, statusCodeForError(err, http.StatusInternalServerError), err.Error())
		return
	}
	if s.config.AutoStartJobs {
		go s.runJob(record)
	}
	writeJSON(writer, http.StatusAccepted, record)
}

func (s *Server) handleGetJob(writer http.ResponseWriter, request *http.Request) {
	jobID := request.PathValue("jobID")
	record, err := s.readJob(jobID)
	if err != nil {
		writeError(writer, statusCodeForError(err, http.StatusNotFound), err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, record)
}

func (s *Server) handleSummaryPath(writer http.ResponseWriter, summaryPath string) {
	payload, err := readJSONFile(summaryPath)
	if err != nil {
		writeError(writer, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, summaryEnvelope(summaryPath, payload))
}

func (s *Server) catalogEntries() []catalogEntry {
	evidenceByTarget := s.reconEvidenceByTarget()
	entries := make([]catalogEntry, 0, len(models))
	for _, model := range models {
		if model.Method != "recon" {
			continue
		}

		entry := catalogEntry{
			Key:             model.AccessLevel + "/" + model.Method + "/" + model.Key,
			AccessLevel:     model.AccessLevel,
			AttackFamily:    model.Method,
			TargetKey:       model.Key,
			Availability:    model.Availability,
			EvidenceLevel:   "catalog",
			Label:           model.Label,
			Paper:           model.Paper,
			Backend:         model.Backend,
			Scheduler:       model.Scheduler,
			BestSummaryPath: nil,
			BestWorkspace:   nil,
		}

		if evidence, ok := evidenceByTarget[model.Key]; ok {
			entry.EvidenceLevel = "best-summary"
			entry.BestSummaryPath = stringPtr(evidence.summaryPath)
			if evidence.workspace != "" {
				entry.BestWorkspace = stringPtr(evidence.workspace)
			}
		}

		entries = append(entries, entry)
	}
	return entries
}

func (s *Server) reconEvidenceByTarget() map[string]catalogEvidence {
	summaryPath, err := s.bestReconSummaryPath()
	if err != nil {
		return map[string]catalogEvidence{}
	}

	payload, err := readJSONFile(summaryPath)
	if err != nil {
		return map[string]catalogEvidence{}
	}

	targetKey := targetKeyForSummary(payload)
	if targetKey == "" {
		return map[string]catalogEvidence{}
	}

	workspace, _ := payload["workspace"].(string)
	return map[string]catalogEvidence{
		targetKey: {
			summaryPath: summaryPath,
			workspace:   workspace,
		},
	}
}

func targetKeyForSummary(payload map[string]any) string {
	method, _ := payload["method"].(string)
	if method != "recon" {
		return ""
	}

	runtime, ok := payload["runtime"].(map[string]any)
	if !ok {
		return ""
	}

	backend, _ := runtime["backend"].(string)
	scheduler, _ := runtime["scheduler"].(string)
	for _, model := range models {
		if model.Method != "recon" || model.Backend != backend {
			continue
		}
		if scheduler != "" {
			if model.Scheduler != nil && *model.Scheduler == scheduler {
				return model.Key
			}
			continue
		}
		if model.Scheduler == nil {
			return model.Key
		}
	}

	return ""
}

func (s *Server) bestReconSummaryPath() (string, error) {
	experimentsRoot, err := requireConfigPath("experiments_root", s.config.ExperimentsRoot, "read experiment summaries")
	if err != nil {
		return "", err
	}
	statusPath := filepath.Join(experimentsRoot, "blackbox-status", "summary.json")
	if payload, err := readJSONFile(statusPath); err == nil {
		methods, ok := payload["methods"].(map[string]any)
		if ok {
			recon, ok := methods["recon"].(map[string]any)
			if ok {
				bestPath, ok := recon["best_evidence_path"].(string)
				if ok && bestPath != "" {
					if _, err := os.Stat(bestPath); err == nil {
						return bestPath, nil
					}
				}
			}
		}
	}

	pattern := filepath.Join(experimentsRoot, "*", "summary.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}

	type candidate struct {
		path        string
		sampleCount float64
	}

	candidates := make([]candidate, 0)
	for _, path := range matches {
		payload, err := readJSONFile(path)
		if err != nil {
			continue
		}
		method, _ := payload["method"].(string)
		if method != "recon" {
			continue
		}
		sampleCount := extractSampleCount(payload)
		candidates = append(candidates, candidate{path: path, sampleCount: sampleCount})
	}
	if len(candidates) == 0 {
		return "", errors.New("no recon experiment summaries found")
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].sampleCount == candidates[j].sampleCount {
			return candidates[i].path < candidates[j].path
		}
		return candidates[i].sampleCount < candidates[j].sampleCount
	})
	return candidates[len(candidates)-1].path, nil
}

func extractSampleCount(payload map[string]any) float64 {
	artifacts, ok := payload["artifacts"].(map[string]any)
	if !ok {
		return 0
	}
	total := 0.0
	for _, value := range artifacts {
		artifact, ok := value.(map[string]any)
		if !ok {
			continue
		}
		sampleCount, ok := artifact["sample_count"].(float64)
		if ok {
			total += sampleCount
		}
	}
	return total
}

func summaryEnvelope(summaryPath string, payload map[string]any) map[string]any {
	backend := any(nil)
	scheduler := any(nil)
	if runtime, ok := payload["runtime"].(map[string]any); ok {
		backend = runtime["backend"]
		scheduler = runtime["scheduler"]
	}
	return map[string]any{
		"status":         payload["status"],
		"paper":          payload["paper"],
		"method":         payload["method"],
		"mode":           payload["mode"],
		"backend":        backend,
		"scheduler":      scheduler,
		"workspace":      payload["workspace"],
		"metrics":        payload["metrics"],
		"artifact_paths": payload["artifact_paths"],
		"summary_path":   summaryPath,
		"raw":            payload,
	}
}

func readJSONFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func validateCreatePayload(payload auditJobCreate) error {
	if payload.JobType == "" {
		return errors.New("job_type is required")
	}
	if payload.WorkspaceName == "" {
		return errors.New("workspace_name is required")
	}
	if strings.Contains(payload.WorkspaceName, "/") || strings.Contains(payload.WorkspaceName, "\\") {
		return errors.New("workspace_name must be a single workspace directory name")
	}
	if payload.JobType == "recon_artifact_mainline" && payload.ArtifactDir == "" {
		return errors.New("recon_artifact_mainline requires artifact_dir")
	}
	return nil
}

func (s *Server) readJob(jobID string) (auditJobRecord, error) {
	var record auditJobRecord
	jobsRoot, err := requireConfigPath("jobs_root", s.config.JobsRoot, "manage audit jobs")
	if err != nil {
		return record, err
	}
	path := filepath.Join(jobsRoot, jobID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return record, err
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, err
	}
	return record, nil
}

func (s *Server) writeJob(record auditJobRecord) error {
	jobsRoot, err := requireConfigPath("jobs_root", s.config.JobsRoot, "manage audit jobs")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(jobsRoot, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(jobsRoot, record.JobID+".json"), data, 0o644)
}

func (s *Server) listJobs() ([]auditJobRecord, error) {
	jobsRoot, err := requireConfigPath("jobs_root", s.config.JobsRoot, "manage audit jobs")
	if err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(filepath.Join(jobsRoot, "*.json"))
	if err != nil {
		return nil, err
	}
	jobs := make([]auditJobRecord, 0, len(matches))
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var record auditJobRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, err
		}
		jobs = append(jobs, record)
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].CreatedAt == jobs[j].CreatedAt {
			return jobs[i].JobID > jobs[j].JobID
		}
		return jobs[i].CreatedAt > jobs[j].CreatedAt
	})
	return jobs, nil
}

func (s *Server) hasActiveWorkspaceJob(workspaceName string) (bool, error) {
	jobs, err := s.listJobs()
	if err != nil {
		return false, err
	}
	for _, job := range jobs {
		if job.WorkspaceName != workspaceName {
			continue
		}
		if job.Status == "queued" || job.Status == "running" {
			return true, nil
		}
	}
	return false, nil
}

func writeJSON(writer http.ResponseWriter, statusCode int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(payload)
}

func writeError(writer http.ResponseWriter, statusCode int, detail string) {
	writeJSON(writer, statusCode, map[string]any{"detail": detail})
}

func requireConfigPath(name, value, purpose string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", configError{message: fmt.Sprintf("%s is required to %s", name, purpose)}
	}
	return value, nil
}

func statusCodeForError(err error, defaultStatus int) int {
	var cfgErr configError
	if errors.As(err, &cfgErr) {
		return http.StatusServiceUnavailable
	}
	return defaultStatus
}

func (s *Server) runJob(record auditJobRecord) {
	record.Status = "running"
	record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_ = s.writeJob(record)

	experimentsRoot, err := requireConfigPath("experiments_root", s.config.ExperimentsRoot, "execute audit jobs")
	if err != nil {
		s.failJob(record, err)
		return
	}
	workspacePath := filepath.Join(experimentsRoot, record.WorkspaceName)
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		s.failJob(record, err)
		return
	}

	executor := s.config.Executor
	if executor == nil {
		executor = s.executePythonJob
	}
	if err := executor(record.Payload, workspacePath); err != nil {
		s.failJob(record, err)
		return
	}

	summaryPath := filepath.Join(workspacePath, "summary.json")
	payload, err := readJSONFile(summaryPath)
	if err != nil {
		s.failJob(record, err)
		return
	}

	record.Status = "completed"
	record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	record.SummaryPath = stringPtr(summaryPath)
	record.Metrics = headlineMetrics(payload)
	record.Error = nil
	record.StdoutTail = nil
	record.StderrTail = nil
	_ = s.writeJob(record)
}

func (s *Server) failJob(record auditJobRecord, err error) {
	message := err.Error()
	record.Status = "failed"
	record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	record.Error = &message
	_ = s.writeJob(record)
}

func (s *Server) executePythonJob(payload auditJobCreate, workspacePath string) error {
	projectRoot, err := requireConfigPath("project_root", s.config.ProjectRoot, "execute audit jobs")
	if err != nil {
		return err
	}
	repoRoot, err := s.resolveRepoRoot(payload)
	if err != nil {
		return err
	}
	if shouldRequestGPU(payload) {
		agent := s.config.GPUAgentPrefix + "-" + payload.WorkspaceName
		release, err := s.acquireGPU(agent)
		if err != nil {
			return err
		}
		if release != nil {
			defer release()
		}
	}

	command := s.pythonCommand(payload, workspacePath, repoRoot)
	if len(command) == 0 {
		return errors.New("empty job command")
	}
	execFn := s.config.ExecCommand
	if execFn == nil {
		execFn = defaultExecCommand
	}
	output, err := execFn(command, projectRoot)
	if err != nil {
		if len(output) > 0 {
			return errors.New(strings.TrimSpace(string(output)))
		}
		return err
	}
	return nil
}

func defaultExecCommand(command []string, dir string) ([]byte, error) {
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func shouldRequestGPU(payload auditJobCreate) bool {
	return payload.JobType != "recon_artifact_mainline"
}

func (s *Server) acquireGPU(agent string) (func(), error) {
	if s.config.AcquireGPU != nil {
		return s.config.AcquireGPU(agent)
	}
	projectRoot, err := requireConfigPath("project_root", s.config.ProjectRoot, "execute audit jobs")
	if err != nil {
		return nil, err
	}
	gpuSchedulerPath := strings.TrimSpace(s.config.GPUSchedulerPath)
	gpuRequestDoc := strings.TrimSpace(s.config.GPURequestDoc)
	if gpuSchedulerPath == "" || gpuRequestDoc == "" {
		return nil, configError{message: "gpu_scheduler and gpu_request_doc are required for GPU jobs"}
	}

	requested := exec.Command(
		gpuSchedulerPath,
		"set-request",
		"--doc", gpuRequestDoc,
		"--agent", agent,
		"--category", "model",
		"--state", "requested",
		"--resource", "gpu",
		"--note", "local-api job",
	)
	requested.Dir = projectRoot
	if output, err := requested.CombinedOutput(); err != nil {
		return nil, errors.New(strings.TrimSpace(string(output)))
	}

	running := exec.Command(
		gpuSchedulerPath,
		"set-request",
		"--doc", gpuRequestDoc,
		"--agent", agent,
		"--category", "model",
		"--state", "running",
		"--resource", "gpu",
		"--note", "local-api job",
	)
	running.Dir = projectRoot
	if output, err := running.CombinedOutput(); err != nil {
		return nil, errors.New(strings.TrimSpace(string(output)))
	}

	return func() {
		release := exec.Command(
			gpuSchedulerPath,
			"release-request",
			"--doc", gpuRequestDoc,
			"--agent", agent,
			"--resource", "gpu",
		)
		release.Dir = projectRoot
		_, _ = release.CombinedOutput()
	}, nil
}

func (s *Server) resolveRepoRoot(payload auditJobCreate) (string, error) {
	if repoRoot := strings.TrimSpace(payload.RepoRoot); repoRoot != "" {
		return repoRoot, nil
	}
	return requireConfigPath("repo_root", s.config.RepoRoot, "execute audit jobs")
}

func (s *Server) pythonCommand(payload auditJobCreate, workspacePath string, repoRoot string) []string {
	method := payload.Method
	if method == "" {
		method = "threshold"
	}
	if payload.JobType == "recon_artifact_mainline" {
		return []string{
			"python",
			"-m",
			"diffaudit",
			"run-recon-artifact-mainline",
			"--artifact-dir",
			payload.ArtifactDir,
			"--workspace",
			workspacePath,
			"--repo-root",
			repoRoot,
			"--method",
			method,
		}
	}
	return []string{
		"python",
		"-m",
		"diffaudit",
		"run-recon-runtime-mainline",
		"--workspace",
		workspacePath,
		"--repo-root",
		repoRoot,
		"--method",
		method,
	}
}

func headlineMetrics(payload map[string]any) map[string]any {
	metrics, ok := payload["metrics"].(map[string]any)
	if !ok {
		return map[string]any{
			"auc":             nil,
			"asr":             nil,
			"tpr_at_1pct_fpr": nil,
		}
	}
	return map[string]any{
		"auc":             metrics["auc"],
		"asr":             metrics["asr"],
		"tpr_at_1pct_fpr": metrics["tpr_at_1pct_fpr"],
	}
}
