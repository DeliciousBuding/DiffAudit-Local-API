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

	"github.com/DeliciousBuding/DiffAudit-Local-API/internal/profiles"
	serviceruntime "github.com/DeliciousBuding/DiffAudit-Local-API/internal/runtime"
)

type Config struct {
	ServiceRoot      string
	RunnersRoot      string
	ExperimentsRoot  string
	JobsRoot         string
	ProjectRoot      string
	RepoRoot         string
	AutoStartJobs    bool
	Executor         func(payload auditJobCreate, workspacePath string) error
	ExecCommand      func(command []string, dir string) ([]byte, error)
	ExecutionMode    string
	DockerBinary     string
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
	JobType        string         `json:"job_type"`
	ContractKey    string         `json:"contract_key"`
	WorkspaceName  string         `json:"workspace_name"`
	RepoRoot       string         `json:"repo_root,omitempty"`
	RuntimeProfile string         `json:"runtime_profile,omitempty"`
	Assets         map[string]any `json:"assets,omitempty"`
	Method         string         `json:"method,omitempty"`
	ArtifactDir    string         `json:"artifact_dir,omitempty"`
	JobInputs      map[string]any `json:"job_inputs,omitempty"`
}

type auditJobRecord struct {
	JobID         string         `json:"job_id"`
	JobType       string         `json:"job_type"`
	ContractKey   string         `json:"contract_key"`
	Status        string         `json:"status"`
	WorkspaceName string         `json:"workspace_name"`
	CreatedAt     string         `json:"created_at"`
	UpdatedAt     string         `json:"updated_at"`
	Payload       auditJobCreate `json:"payload"`
	Command       []string       `json:"command"`
	SummaryPath   *string        `json:"summary_path"`
	Metrics       any            `json:"metrics"`
	Error         *string        `json:"error"`
	OutputCapture *string        `json:"output_capture,omitempty"`
	OutputTail    []string       `json:"output_tail,omitempty"`
	StdoutTail    []string       `json:"stdout_tail"`
	StderrTail    []string       `json:"stderr_tail"`
}

type catalogEntry struct {
	ContractKey      string  `json:"contract_key"`
	Track            string  `json:"track"`
	AttackFamily     string  `json:"attack_family"`
	TargetKey        string  `json:"target_key"`
	Availability     string  `json:"availability"`
	EvidenceLevel    string  `json:"evidence_level"`
	Label            string  `json:"label"`
	Paper            string  `json:"paper"`
	Backend          string  `json:"backend"`
	Scheduler        *string `json:"scheduler"`
	BestSummaryPath  *string `json:"best_summary_path"`
	BestWorkspace    *string `json:"best_workspace"`
	AdmissionStatus  *string `json:"admission_status,omitempty"`
	AdmissionLevel   *string `json:"admission_level,omitempty"`
	ProvenanceStatus *string `json:"provenance_status,omitempty"`
	IntakeManifest   *string `json:"intake_manifest,omitempty"`
}

type catalogEvidence struct {
	summaryPath string
	workspace   string
}

type intakeIndex struct {
	Entries []intakeEntry `json:"entries"`
}

type intakeEntry struct {
	ContractKey string         `json:"contract_key"`
	Manifest    string         `json:"manifest"`
	Admission   map[string]any `json:"admission"`
}

type configError struct {
	message string
}

func (err configError) Error() string {
	return err.message
}

type commandExecutionError struct {
	message    string
	command    []string
	outputTail []string
	stdoutTail []string
	stderrTail []string
}

func (err commandExecutionError) Error() string {
	return err.message
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
	mux.HandleFunc("GET /diagnostics", server.handleDiagnostics)
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

func (s *Server) handleDiagnostics(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{
		"status":          "ok",
		"execution_mode":  strings.TrimSpace(s.config.ExecutionMode),
		"docker_binary":   strings.TrimSpace(s.config.DockerBinary),
		"gpu_scheduler":   strings.TrimSpace(s.config.GPUSchedulerPath),
		"gpu_request_doc": strings.TrimSpace(s.config.GPURequestDoc),
		"paths": map[string]any{
			"service_root":     describePath(s.config.ServiceRoot),
			"runners_root":     describePath(s.config.RunnersRoot),
			"experiments_root": describePath(s.config.ExperimentsRoot),
			"jobs_root":        describePath(s.config.JobsRoot),
			"project_root":     describePath(s.config.ProjectRoot),
			"repo_root":        describePath(s.config.RepoRoot),
		},
	})
}

func (s *Server) handleModels(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, liveModelOptions())
}

func (s *Server) handleCatalog(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, s.catalogEntries())
}

func (s *Server) handleBestRecon(writer http.ResponseWriter, _ *http.Request) {
	definition, ok := contractDefinitionByKey("black-box/recon/sd15-ddim")
	if !ok {
		writeError(writer, http.StatusNotFound, "missing live recon contract")
		return
	}
	summaryPath, err := s.bestSummaryPathForContract(definition)
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
	payload = normalizeCreatePayload(payload)
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
		ContractKey:   payload.ContractKey,
		Status:        "queued",
		WorkspaceName: payload.WorkspaceName,
		CreatedAt:     now,
		UpdatedAt:     now,
		Payload:       payload,
		Command:       nil,
		SummaryPath:   nil,
		Metrics:       nil,
		Error:         nil,
		OutputCapture: nil,
		OutputTail:    nil,
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
	definitions := catalogContractDefinitions()
	intakeByContract := s.intakeEntriesByContract()
	entries := make([]catalogEntry, 0, len(definitions))
	for _, definition := range definitions {
		entry := projectCatalogEntry(definition)
		if intake, ok := intakeByContract[definition.ContractKey]; ok {
			applyIntakeMetadata(&entry, intake)
		}

		if evidence, ok := s.catalogEvidenceForContract(definition); ok {
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

func (s *Server) intakeEntriesByContract() map[string]intakeEntry {
	projectRoot := strings.TrimSpace(s.config.ProjectRoot)
	if projectRoot == "" {
		return nil
	}
	indexPath := filepath.Join(projectRoot, "workspaces", "intake", "index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil
	}
	var index intakeIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil
	}
	if len(index.Entries) == 0 {
		return nil
	}
	result := make(map[string]intakeEntry, len(index.Entries))
	for _, entry := range index.Entries {
		if strings.TrimSpace(entry.ContractKey) == "" {
			continue
		}
		result[entry.ContractKey] = entry
	}
	return result
}

func applyIntakeMetadata(entry *catalogEntry, intake intakeEntry) {
	if entry == nil {
		return
	}
	if manifest := strings.TrimSpace(intake.Manifest); manifest != "" {
		entry.IntakeManifest = stringPtr(manifest)
	}
	if status, ok := intake.Admission["status"].(string); ok && strings.TrimSpace(status) != "" {
		entry.AdmissionStatus = stringPtr(strings.TrimSpace(status))
	}
	if level, ok := intake.Admission["level"].(string); ok && strings.TrimSpace(level) != "" {
		entry.AdmissionLevel = stringPtr(strings.TrimSpace(level))
	}
	if provenance, ok := intake.Admission["provenance_status"].(string); ok && strings.TrimSpace(provenance) != "" {
		entry.ProvenanceStatus = stringPtr(strings.TrimSpace(provenance))
	}
}

func (s *Server) catalogEvidenceForContract(definition contractDefinition) (catalogEvidence, bool) {
	summaryPath, err := s.bestSummaryPathForContract(definition)
	if err != nil {
		return catalogEvidence{}, false
	}
	payload, err := readJSONFile(summaryPath)
	if err != nil {
		return catalogEvidence{}, false
	}
	workspace, _ := payload["workspace"].(string)
	return catalogEvidence{
		summaryPath: summaryPath,
		workspace:   workspace,
	}, true
}

func (s *Server) bestSummaryPathForContract(definition contractDefinition) (string, error) {
	if definition.StatusMethodKey != "" {
		if path, err := s.bestSummaryPathFromStatus(definition); err == nil {
			return path, nil
		}
	}
	return s.bestSummaryPathByScan(definition)
}

func (s *Server) bestSummaryPathFromStatus(definition contractDefinition) (string, error) {
	experimentsRoot, err := requireConfigPath("experiments_root", s.config.ExperimentsRoot, "read experiment summaries")
	if err != nil {
		return "", err
	}
	statusPath := filepath.Join(experimentsRoot, "blackbox-status", "summary.json")
	if payload, err := readJSONFile(statusPath); err == nil {
		methods, ok := payload["methods"].(map[string]any)
		if ok {
			methodEntry, ok := methods[definition.StatusMethodKey].(map[string]any)
			if ok {
				bestPath, ok := methodEntry["best_evidence_path"].(string)
				if ok && bestPath != "" {
					if _, err := os.Stat(bestPath); err == nil {
						return bestPath, nil
					}
				}
			}
		}
	}
	return "", errors.New("no status-backed summary found for contract")
}

func (s *Server) bestSummaryPathByScan(definition contractDefinition) (string, error) {
	experimentsRoot, err := requireConfigPath("experiments_root", s.config.ExperimentsRoot, "read experiment summaries")
	if err != nil {
		return "", err
	}
	patterns := []string{
		filepath.Join(experimentsRoot, "*", "summary.json"),
	}
	if projectRoot := strings.TrimSpace(s.config.ProjectRoot); projectRoot != "" {
		patterns = append(patterns,
			filepath.Join(projectRoot, "workspaces", "gray-box", "runs", "*", "summary.json"),
			filepath.Join(projectRoot, "workspaces", "white-box", "runs", "*", "summary.json"),
		)
	}
	matches := make([]string, 0)
	for _, pattern := range patterns {
		found, globErr := filepath.Glob(pattern)
		if globErr != nil {
			return "", globErr
		}
		matches = append(matches, found...)
	}

	type candidate struct {
		path        string
		sampleCount float64
	}

	candidates := make([]candidate, 0)
	for _, path := range matches {
		if !summaryMatchesContract(definition, path) {
			continue
		}
		payload, err := readJSONFile(path)
		if err != nil {
			continue
		}
		sampleCount := extractSampleCount(payload)
		candidates = append(candidates, candidate{path: path, sampleCount: sampleCount})
	}
	if len(candidates) == 0 {
		return "", errors.New("no experiment summaries found for contract")
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].sampleCount == candidates[j].sampleCount {
			return candidates[i].path < candidates[j].path
		}
		return candidates[i].sampleCount < candidates[j].sampleCount
	})
	return candidates[len(candidates)-1].path, nil
}

func summaryMatchesContract(definition contractDefinition, summaryPath string) bool {
	payload, err := readJSONFile(summaryPath)
	if err != nil {
		return false
	}
	method, _ := payload["method"].(string)
	if method != definition.AttackFamily {
		return false
	}
	runtime, ok := payload["runtime"].(map[string]any)
	if !ok {
		return definition.Backend == ""
	}
	backend, _ := runtime["backend"].(string)
	if definition.Backend == "" {
		return true
	}
	if backend != definition.Backend {
		return false
	}
	scheduler, _ := runtime["scheduler"].(string)
	if definition.Scheduler != nil {
		return scheduler == *definition.Scheduler
	}
	return scheduler == ""
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
	contractKey := any(nil)
	attackFamily := any(nil)
	targetKey := any(nil)
	if runtime, ok := payload["runtime"].(map[string]any); ok {
		backend = runtime["backend"]
		scheduler = runtime["scheduler"]
	}
	track, _ := payload["track"].(string)
	if definition, ok := contractForSummaryPayload(payload); ok {
		projection := projectContract(definition)
		contractKey = projection.ContractKey
		attackFamily = projection.AttackFamily
		targetKey = projection.TargetKey
		if track == "" {
			track = projection.Track
		}
	}
	return map[string]any{
		"status":         payload["status"],
		"track":          track,
		"contract_key":   contractKey,
		"attack_family":  attackFamily,
		"target_key":     targetKey,
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
	if payload.ContractKey == "" {
		return errors.New("contract_key is required")
	}
	if payload.WorkspaceName == "" {
		return errors.New("workspace_name is required")
	}
	if strings.Contains(payload.WorkspaceName, "/") || strings.Contains(payload.WorkspaceName, "\\") {
		return errors.New("workspace_name must be a single workspace directory name")
	}
	if err := validateRuntimeProfile(payload.RuntimeProfile); err != nil {
		return err
	}
	job, definition, ok := liveJobDefinition(payload.JobType)
	if !ok {
		return errors.New("unsupported job_type")
	}
	if payload.ContractKey != definition.ContractKey {
		return errors.New("contract_key does not match job_type")
	}
	for _, key := range job.RequiredInputs {
		if value := strings.TrimSpace(jobInputString(payload, key)); value == "" {
			return errors.New(payload.JobType + " requires " + key)
		}
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

func describePath(path string) map[string]any {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return map[string]any{
			"path":   "",
			"exists": false,
		}
	}
	_, err := os.Stat(trimmed)
	return map[string]any{
		"path":   trimmed,
		"exists": err == nil,
	}
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
	if executor := s.config.Executor; executor == nil {
		if command, err := s.plannedJobCommand(record.Payload, filepath.Join(s.config.ExperimentsRoot, record.WorkspaceName)); err == nil {
			record.Command = command
		}
	}
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
	record.OutputCapture = nil
	record.OutputTail = nil
	record.StdoutTail = nil
	record.StderrTail = nil
	_ = s.writeJob(record)
}

func (s *Server) failJob(record auditJobRecord, err error) {
	message := err.Error()
	record.Status = "failed"
	record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	record.Error = &message
	var execErr commandExecutionError
	if errors.As(err, &execErr) {
		if len(execErr.command) > 0 {
			record.Command = execErr.command
		}
		if len(execErr.outputTail) > 0 {
			record.OutputCapture = stringPtr("combined")
			record.OutputTail = execErr.outputTail
		}
		if len(execErr.stdoutTail) > 0 {
			record.StdoutTail = execErr.stdoutTail
		}
		if len(execErr.stderrTail) > 0 {
			record.StderrTail = execErr.stderrTail
		}
	}
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

	spec, err := s.buildExecutionSpec(payload, workspacePath, projectRoot, repoRoot)
	if err != nil {
		return err
	}
	executor := s.executionExecutorForPayload(payload)
	output, err := executor.Execute(spec)
	if err != nil {
		return commandExecutionError{
			message:    commandFailureMessage(err, output),
			command:    mustBuildCommand(executor, spec),
			outputTail: outputTailLines(output),
		}
	}
	return nil
}

func defaultExecCommand(command []string, dir string) ([]byte, error) {
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func shouldRequestGPU(payload auditJobCreate) bool {
	job, _, ok := liveJobDefinition(payload.JobType)
	if !ok {
		return true
	}
	return job.RequestsGPU
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

func (s *Server) plannedJobCommand(payload auditJobCreate, workspacePath string) ([]string, error) {
	projectRoot, err := requireConfigPath("project_root", s.config.ProjectRoot, "execute audit jobs")
	if err != nil {
		return nil, err
	}
	repoRoot, err := s.resolveRepoRoot(payload)
	if err != nil {
		return nil, err
	}
	spec, err := s.buildExecutionSpec(payload, workspacePath, projectRoot, repoRoot)
	if err != nil {
		return nil, err
	}
	return s.executionExecutorForPayload(payload).BuildCommand(spec)
}

func (s *Server) buildExecutionSpec(
	payload auditJobCreate,
	workspacePath string,
	projectRoot string,
	repoRoot string,
) (serviceruntime.ExecutionSpec, error) {
	return profiles.BuildSpec(profiles.JobRequest{
		JobType:       payload.JobType,
		RuntimeTarget: s.runtimeTargetForPayload(payload),
		ServiceRoot:   s.config.ServiceRoot,
		RunnersRoot:   s.config.RunnersRoot,
		ProjectRoot:   projectRoot,
		RepoRoot:      repoRoot,
		WorkspacePath: workspacePath,
		Inputs:        normalizedJobInputs(payload),
	})
}

func (s *Server) runtimeTarget() profiles.RuntimeTarget {
	if strings.EqualFold(strings.TrimSpace(s.config.ExecutionMode), "docker") {
		return profiles.RuntimeTargetDocker
	}
	return profiles.RuntimeTargetLocal
}

func (s *Server) runtimeTargetForPayload(payload auditJobCreate) profiles.RuntimeTarget {
	switch normalizeRuntimeProfile(payload.RuntimeProfile) {
	case "docker", "docker-default":
		return profiles.RuntimeTargetDocker
	case "local", "local-default":
		return profiles.RuntimeTargetLocal
	default:
		return s.runtimeTarget()
	}
}

func (s *Server) executionExecutor() serviceruntime.Executor {
	return s.executionExecutorForTarget(s.runtimeTarget())
}

func (s *Server) executionExecutorForPayload(payload auditJobCreate) serviceruntime.Executor {
	return s.executionExecutorForTarget(s.runtimeTargetForPayload(payload))
}

func (s *Server) executionExecutorForTarget(target profiles.RuntimeTarget) serviceruntime.Executor {
	execFn := s.config.ExecCommand
	if execFn == nil {
		execFn = defaultExecCommand
	}
	if target == profiles.RuntimeTargetDocker {
		return serviceruntime.DockerExecutor{
			Binary:      strings.TrimSpace(s.config.DockerBinary),
			ExecCommand: execFn,
		}
	}
	return serviceruntime.LocalExecutor{ExecCommand: execFn}
}

func mustBuildCommand(executor serviceruntime.Executor, spec serviceruntime.ExecutionSpec) []string {
	command, err := executor.BuildCommand(spec)
	if err != nil {
		return nil
	}
	return command
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
	result := cloneMap(metrics)
	if _, ok := result["auc"]; !ok {
		result["auc"] = nil
	}
	if _, ok := result["asr"]; !ok {
		result["asr"] = nil
	}
	if _, ok := result["tpr_at_1pct_fpr"]; !ok {
		result["tpr_at_1pct_fpr"] = nil
	}
	return result
}

func normalizeCreatePayload(payload auditJobCreate) auditJobCreate {
	inputs := cloneMap(payload.JobInputs)
	payload.RuntimeProfile = normalizeRuntimeProfile(payload.RuntimeProfile)
	if payload.ArtifactDir != "" {
		if _, ok := inputs["artifact_dir"]; !ok {
			inputs["artifact_dir"] = payload.ArtifactDir
		}
	}
	if payload.Method != "" {
		if _, ok := inputs["method"]; !ok {
			inputs["method"] = payload.Method
		}
	}
	mergeInputSources(inputs, payload.Assets)
	if len(inputs) == 0 {
		payload.JobInputs = nil
		return payload
	}
	payload.JobInputs = inputs
	return payload
}

func jobInputString(payload auditJobCreate, key string) string {
	inputs := normalizedJobInputs(payload)
	if value, ok := inputs[key]; ok {
		if str, ok := value.(string); ok {
			return strings.TrimSpace(str)
		}
	}
	return ""
}

func normalizedJobInputs(payload auditJobCreate) map[string]any {
	inputs := cloneMap(payload.JobInputs)
	if payload.ArtifactDir != "" {
		if _, ok := inputs["artifact_dir"]; !ok {
			inputs["artifact_dir"] = payload.ArtifactDir
		}
	}
	if payload.Method != "" {
		if _, ok := inputs["method"]; !ok {
			inputs["method"] = payload.Method
		}
	}
	mergeInputSources(inputs, payload.Assets)
	return inputs
}

func mergeInputSources(inputs map[string]any, sources ...map[string]any) {
	for _, source := range sources {
		if len(source) == 0 {
			continue
		}
		for key, value := range source {
			switch key {
			case "runtime", "orchestrator":
				if nested, ok := value.(map[string]any); ok {
					mergeInputSources(inputs, nested)
				}
				continue
			}
			if _, ok := inputs[key]; ok {
				continue
			}
			inputs[key] = value
		}
	}
}

func normalizeRuntimeProfile(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validateRuntimeProfile(value string) error {
	switch normalizeRuntimeProfile(value) {
	case "", "local", "local-default", "docker", "docker-default":
		return nil
	default:
		return errors.New("runtime_profile must be one of local, local-default, docker, docker-default")
	}
}

func cloneMap(value map[string]any) map[string]any {
	if len(value) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}

func commandFailureMessage(err error, output []byte) string {
	if len(output) > 0 {
		return strings.TrimSpace(string(output))
	}
	return err.Error()
}

func outputTailLines(output []byte) []string {
	if len(output) == 0 {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			filtered = append(filtered, line)
		}
	}
	if len(filtered) <= 10 {
		return filtered
	}
	return filtered[len(filtered)-10:]
}
