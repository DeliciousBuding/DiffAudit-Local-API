package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeJSONFile(t *testing.T, path string, payload any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}

func decodeJSONResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	return payload
}

func decodeJSONArrayResponse(t *testing.T, recorder *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var payload []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	return payload
}

func findCatalogEntry(t *testing.T, payload []map[string]any, attackFamily string, targetKey string) map[string]any {
	t.Helper()
	for _, item := range payload {
		if item["attack_family"] == attackFamily && item["target_key"] == targetKey {
			return item
		}
	}
	t.Fatalf("missing catalog entry attack_family=%s target_key=%s", attackFamily, targetKey)
	return nil
}

func findContractDefinition(t *testing.T, contractKey string) contractDefinition {
	t.Helper()
	for _, definition := range contractRegistry {
		if definition.ContractKey == contractKey {
			return definition
		}
	}
	t.Fatalf("missing contract definition %s", contractKey)
	return contractDefinition{}
}

func TestHealthEndpoint(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	payload := decodeJSONResponse(t, recorder)
	if payload["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", payload["status"])
	}
}

func TestRequestLoggingMiddlewareRecordsMethodPathAndStatus(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuffer, &slog.HandlerOptions{}))

	handler := requestLoggingMiddleware(logger, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusAccepted)
	}))

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", recorder.Code)
	}

	logOutput := logBuffer.String()
	for _, want := range []string{
		"request completed",
		"method=GET",
		"path=/health",
		"status=202",
	} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("log output missing %q:\n%s", want, logOutput)
		}
	}
}

func TestShouldRequestGPU(t *testing.T) {
	if shouldRequestGPU(auditJobCreate{JobType: "recon_artifact_mainline"}) {
		t.Fatalf("recon_artifact_mainline should not request gpu")
	}
	if !shouldRequestGPU(auditJobCreate{JobType: "recon_runtime_mainline"}) {
		t.Fatalf("recon_runtime_mainline should request gpu")
	}
}

func TestNewServerDoesNotFillProjectRootFallback(t *testing.T) {
	server := NewServer(Config{})

	if server.config.ProjectRoot != "" {
		t.Fatalf("expected empty project root, got %s", server.config.ProjectRoot)
	}
}

func TestExecutePythonJobRequiresProjectRoot(t *testing.T) {
	server := NewServer(Config{
		RepoRoot: "D:/repo",
	})

	err := server.executePythonJob(
		auditJobCreate{
			JobType:       "recon_artifact_mainline",
			WorkspaceName: "workspace-a",
		},
		"D:/workspace-a",
	)
	if err == nil {
		t.Fatal("expected missing project root error")
	}
	if !strings.Contains(err.Error(), "project_root") {
		t.Fatalf("expected project_root error, got %v", err)
	}
}

func TestExecutePythonJobRequiresRepoRoot(t *testing.T) {
	server := NewServer(Config{
		ProjectRoot: "D:/project",
	})

	err := server.executePythonJob(
		auditJobCreate{
			JobType:       "recon_artifact_mainline",
			WorkspaceName: "workspace-a",
		},
		"D:/workspace-a",
	)
	if err == nil {
		t.Fatal("expected missing repo root error")
	}
	if !strings.Contains(err.Error(), "repo_root") {
		t.Fatalf("expected repo_root error, got %v", err)
	}
}

func TestExecutePythonJobRequiresGPUPathsForRuntimeJobs(t *testing.T) {
	server := NewServer(Config{
		ProjectRoot: "D:/project",
		RepoRoot:    "D:/repo",
	})

	err := server.executePythonJob(
		auditJobCreate{
			JobType:       "recon_runtime_mainline",
			WorkspaceName: "workspace-a",
		},
		"D:/workspace-a",
	)
	if err == nil {
		t.Fatal("expected missing gpu configuration error")
	}
	if !strings.Contains(err.Error(), "gpu_scheduler") {
		t.Fatalf("expected gpu_scheduler error, got %v", err)
	}
	if !strings.Contains(err.Error(), "gpu_request_doc") {
		t.Fatalf("expected gpu_request_doc error, got %v", err)
	}
}

func TestExecutePythonJobUsesConfiguredRepoRoot(t *testing.T) {
	root := t.TempDir()
	var gotCommand []string
	var gotDir string

	server := NewServer(Config{
		ProjectRoot: root,
		RepoRoot:    "D:/repo-from-config",
		ExecCommand: func(command []string, dir string) ([]byte, error) {
			gotCommand = append([]string(nil), command...)
			gotDir = dir
			return []byte("ok"), nil
		},
	})

	err := server.executePythonJob(
		auditJobCreate{
			JobType:       "recon_artifact_mainline",
			WorkspaceName: "workspace-a",
			ArtifactDir:   "D:/artifact",
		},
		filepath.Join(root, "workspace-a"),
	)
	if err != nil {
		t.Fatalf("executePythonJob returned error: %v", err)
	}
	if gotDir != root {
		t.Fatalf("expected command dir %s, got %s", root, gotDir)
	}
	commandLine := strings.Join(gotCommand, "\n")
	if !strings.Contains(commandLine, "--repo-root\nD:/repo-from-config") {
		t.Fatalf("expected configured repo root in command, got %v", gotCommand)
	}
}

func TestExecutePythonJobAcquiresAndReleasesGPU(t *testing.T) {
	root := t.TempDir()
	acquired := 0
	released := 0
	executed := 0

	server := NewServer(Config{
		ProjectRoot: root,
		RepoRoot:    "D:/repo",
		AcquireGPU: func(agent string) (func(), error) {
			acquired++
			if agent != "local-api-workspace-a" {
				t.Fatalf("unexpected agent name: %s", agent)
			}
			return func() {
				released++
			}, nil
		},
		ExecCommand: func(command []string, dir string) ([]byte, error) {
			executed++
			return []byte("ok"), nil
		},
	})

	err := server.executePythonJob(
		auditJobCreate{
			JobType:       "recon_runtime_mainline",
			WorkspaceName: "workspace-a",
		},
		filepath.Join(root, "workspace-a"),
	)
	if err != nil {
		t.Fatalf("executePythonJob returned error: %v", err)
	}
	if acquired != 1 {
		t.Fatalf("expected 1 gpu acquire, got %d", acquired)
	}
	if released != 1 {
		t.Fatalf("expected 1 gpu release, got %d", released)
	}
	if executed != 1 {
		t.Fatalf("expected command execution once, got %d", executed)
	}
}

func TestModelsEndpoint(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/models", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	var payload []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(payload) < 3 {
		t.Fatalf("expected at least 3 models, got %d", len(payload))
	}
}

func TestContractRegistryIncludesTargetGrayAndWhiteContracts(t *testing.T) {
	pia := findContractDefinition(t, "gray-box/pia/cifar10-ddpm")
	if pia.ContractStatus != "target" {
		t.Fatalf("expected pia contract_status target, got %s", pia.ContractStatus)
	}
	if pia.CatalogVisible {
		t.Fatalf("expected pia target contract to stay out of live catalog")
	}
	if len(pia.Jobs) != 0 {
		t.Fatalf("expected pia target contract to have no live jobs, got %d", len(pia.Jobs))
	}
	if pia.FeatureAccess != "epsilon_t" {
		t.Fatalf("expected pia feature_access epsilon_t, got %s", pia.FeatureAccess)
	}
	if len(pia.LivePromotionGates) == 0 {
		t.Fatal("expected pia live promotion gates")
	}

	gsa := findContractDefinition(t, "white-box/gsa/ddpm-cifar10")
	if gsa.ContractStatus != "target" {
		t.Fatalf("expected gsa contract_status target, got %s", gsa.ContractStatus)
	}
	if gsa.CatalogVisible {
		t.Fatalf("expected gsa target contract to stay out of live catalog")
	}
	if len(gsa.Jobs) != 0 {
		t.Fatalf("expected gsa target contract to have no live jobs, got %d", len(gsa.Jobs))
	}
	if gsa.FeatureAccess != "gradient" {
		t.Fatalf("expected gsa feature_access gradient, got %s", gsa.FeatureAccess)
	}
	if len(gsa.LivePromotionGates) == 0 {
		t.Fatal("expected gsa live promotion gates")
	}
}

func TestLiveJobRegistryStaysBoundToBlackBoxRecon(t *testing.T) {
	if _, _, ok := liveJobDefinition("recon_artifact_mainline"); !ok {
		t.Fatal("expected live recon artifact job definition")
	}
	if _, _, ok := liveJobDefinition("pia_runtime_probe"); ok {
		t.Fatal("did not expect gray-box target job to be live")
	}
}

func TestCatalogEndpointReturnsStaticReconEntriesWithoutEvidence(t *testing.T) {
	server := NewServer(Config{})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	payload := decodeJSONArrayResponse(t, recorder)
	if len(payload) != 2 {
		t.Fatalf("expected 2 recon catalog entries, got %d", len(payload))
	}

	entry := findCatalogEntry(t, payload, "recon", "sd15-ddim")
	if entry["contract_key"] != "black-box/recon/sd15-ddim" {
		t.Fatalf("expected recon contract_key, got %v", entry["contract_key"])
	}
	if entry["track"] != "black-box" {
		t.Fatalf("expected black-box track, got %v", entry["track"])
	}
	if entry["availability"] != "ready" {
		t.Fatalf("expected ready availability, got %v", entry["availability"])
	}
	if entry["evidence_level"] != "catalog" {
		t.Fatalf("expected catalog evidence_level, got %v", entry["evidence_level"])
	}
	if entry["best_summary_path"] != nil {
		t.Fatalf("expected nil best_summary_path, got %v", entry["best_summary_path"])
	}
	if _, ok := entry["access_level"]; ok {
		t.Fatalf("expected access_level to be removed from catalog entry, got %v", entry["access_level"])
	}
}

func TestCatalogEndpointHydratesBestReconEvidenceWhenAvailable(t *testing.T) {
	root := t.TempDir()
	experimentsRoot := filepath.Join(root, "experiments")
	bestWorkspace := filepath.Join(experimentsRoot, "recon-runtime-mainline-ddim-public-50-step10")

	writeJSONFile(t, filepath.Join(bestWorkspace, "summary.json"), map[string]any{
		"status":    "ready",
		"paper":     "BlackBox_Reconstruction_ArXiv2023",
		"method":    "recon",
		"mode":      "runtime-mainline",
		"workspace": bestWorkspace,
		"runtime": map[string]any{
			"backend":   "stable_diffusion",
			"scheduler": "ddim",
		},
		"metrics": map[string]any{
			"auc":             0.866,
			"asr":             0.51,
			"tpr_at_1pct_fpr": 1.0,
		},
		"artifact_paths": map[string]any{
			"summary": filepath.Join(bestWorkspace, "summary.json"),
		},
	})

	writeJSONFile(t, filepath.Join(experimentsRoot, "blackbox-status", "summary.json"), map[string]any{
		"status": "ready",
		"track":  "black-box",
		"methods": map[string]any{
			"recon": map[string]any{
				"best_evidence_path": filepath.Join(bestWorkspace, "summary.json"),
			},
		},
	})

	server := NewServer(Config{
		ExperimentsRoot: experimentsRoot,
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	payload := decodeJSONArrayResponse(t, recorder)
	entry := findCatalogEntry(t, payload, "recon", "sd15-ddim")
	if entry["evidence_level"] != "best-summary" {
		t.Fatalf("expected best-summary evidence_level, got %v", entry["evidence_level"])
	}
	if entry["best_summary_path"] != filepath.Join(bestWorkspace, "summary.json") {
		t.Fatalf("expected best_summary_path to point at best recon summary, got %v", entry["best_summary_path"])
	}
	if entry["best_workspace"] != bestWorkspace {
		t.Fatalf("expected best_workspace %s, got %v", bestWorkspace, entry["best_workspace"])
	}

	kandinsky := findCatalogEntry(t, payload, "recon", "kandinsky-v22")
	if kandinsky["evidence_level"] != "catalog" {
		t.Fatalf("expected unmatched entry to stay catalog evidence, got %v", kandinsky["evidence_level"])
	}
}

func TestBestReconEndpoint(t *testing.T) {
	root := t.TempDir()
	experimentsRoot := filepath.Join(root, "experiments")
	bestWorkspace := filepath.Join(experimentsRoot, "recon-runtime-mainline-ddim-public-50-step10")

	writeJSONFile(t, filepath.Join(bestWorkspace, "summary.json"), map[string]any{
		"status":    "ready",
		"paper":     "BlackBox_Reconstruction_ArXiv2023",
		"method":    "recon",
		"mode":      "runtime-mainline",
		"workspace": bestWorkspace,
		"runtime": map[string]any{
			"backend":   "stable_diffusion",
			"scheduler": "ddim",
		},
		"metrics": map[string]any{
			"auc":             0.866,
			"asr":             0.51,
			"tpr_at_1pct_fpr": 1.0,
		},
		"artifact_paths": map[string]any{
			"summary": filepath.Join(bestWorkspace, "summary.json"),
		},
	})

	writeJSONFile(t, filepath.Join(experimentsRoot, "blackbox-status", "summary.json"), map[string]any{
		"status": "ready",
		"track":  "black-box",
		"methods": map[string]any{
			"recon": map[string]any{
				"best_evidence_path": filepath.Join(bestWorkspace, "summary.json"),
			},
		},
	})

	server := NewServer(Config{
		ExperimentsRoot: experimentsRoot,
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/experiments/recon/best", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	payload := decodeJSONResponse(t, recorder)
	if payload["workspace"] != bestWorkspace {
		t.Fatalf("expected workspace %s, got %v", bestWorkspace, payload["workspace"])
	}
}

func TestBestReconEndpointPrefersBlackboxStatusSourceOfTruth(t *testing.T) {
	root := t.TempDir()
	experimentsRoot := filepath.Join(root, "experiments")
	statusWorkspace := filepath.Join(experimentsRoot, "recon-runtime-mainline-ddim-public-50-step10")
	largerWorkspace := filepath.Join(experimentsRoot, "recon-runtime-mainline-ddim-public-100-step10")

	writeJSONFile(t, filepath.Join(statusWorkspace, "summary.json"), map[string]any{
		"status":    "ready",
		"paper":     "BlackBox_Reconstruction_ArXiv2023",
		"method":    "recon",
		"mode":      "runtime-mainline",
		"workspace": statusWorkspace,
		"metrics": map[string]any{
			"auc": 0.866,
		},
		"artifact_paths": map[string]any{
			"summary": filepath.Join(statusWorkspace, "summary.json"),
		},
	})
	writeJSONFile(t, filepath.Join(largerWorkspace, "summary.json"), map[string]any{
		"status":    "ready",
		"paper":     "BlackBox_Reconstruction_ArXiv2023",
		"method":    "recon",
		"mode":      "runtime-mainline",
		"workspace": largerWorkspace,
		"artifacts": map[string]any{
			"target_member":     map[string]any{"sample_count": 100.0},
			"target_non_member": map[string]any{"sample_count": 100.0},
			"shadow_member":     map[string]any{"sample_count": 100.0},
			"shadow_non_member": map[string]any{"sample_count": 100.0},
		},
		"metrics": map[string]any{
			"auc": 0.788,
		},
		"artifact_paths": map[string]any{
			"summary": filepath.Join(largerWorkspace, "summary.json"),
		},
	})

	writeJSONFile(t, filepath.Join(experimentsRoot, "blackbox-status", "summary.json"), map[string]any{
		"status": "ready",
		"track":  "black-box",
		"methods": map[string]any{
			"recon": map[string]any{
				"best_evidence_path": filepath.Join(statusWorkspace, "summary.json"),
			},
		},
	})

	server := NewServer(Config{
		ExperimentsRoot: experimentsRoot,
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/experiments/recon/best", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	payload := decodeJSONResponse(t, recorder)
	if payload["workspace"] != statusWorkspace {
		t.Fatalf("expected blackbox-status workspace %s, got %v", statusWorkspace, payload["workspace"])
	}
}

func TestWorkspaceSummaryEndpoint(t *testing.T) {
	root := t.TempDir()
	experimentsRoot := filepath.Join(root, "experiments")
	workspace := filepath.Join(experimentsRoot, "recon-runtime-mainline-ddim-public-50-step10")

	writeJSONFile(t, filepath.Join(workspace, "summary.json"), map[string]any{
		"status":    "ready",
		"paper":     "BlackBox_Reconstruction_ArXiv2023",
		"method":    "recon",
		"mode":      "runtime-mainline",
		"workspace": workspace,
		"metrics": map[string]any{
			"auc":             0.866,
			"asr":             0.51,
			"tpr_at_1pct_fpr": 1.0,
		},
		"artifact_paths": map[string]any{
			"summary": filepath.Join(workspace, "summary.json"),
		},
	})

	server := NewServer(Config{
		ExperimentsRoot: experimentsRoot,
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/experiments/recon-runtime-mainline-ddim-public-50-step10/summary",
		nil,
	)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	payload := decodeJSONResponse(t, recorder)
	if payload["track"] != "black-box" {
		t.Fatalf("expected inferred black-box track, got %v", payload["track"])
	}
	metrics, ok := payload["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("expected metrics object, got %T", payload["metrics"])
	}
	if metrics["tpr_at_1pct_fpr"] != 1.0 {
		t.Fatalf("expected tpr_at_1pct_fpr 1.0, got %v", metrics["tpr_at_1pct_fpr"])
	}
}

func TestCreateAndGetJobEndpoints(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	requestBody := map[string]any{
		"job_type":       "recon_artifact_mainline",
		"contract_key":   "black-box/recon/sd15-ddim",
		"workspace_name": "api-job-001",
		"artifact_dir":   "D:/artifacts/recon-scores",
		"repo_root":      "D:/Code/DiffAudit/Project/external/Reconstruction-based-Attack",
	}
	data, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/audit/jobs", bytes.NewReader(data))
	createRequest.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(createRecorder, createRequest)

	if createRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", createRecorder.Code)
	}

	created := decodeJSONResponse(t, createRecorder)
	jobID, ok := created["job_id"].(string)
	if !ok || jobID == "" {
		t.Fatalf("expected job_id, got %v", created["job_id"])
	}

	jobRequest := httptest.NewRequest(http.MethodGet, "/api/v1/audit/jobs/"+jobID, nil)
	jobRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(jobRecorder, jobRequest)

	if jobRecorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", jobRecorder.Code)
	}
	jobPayload := decodeJSONResponse(t, jobRecorder)
	if jobPayload["contract_key"] != "black-box/recon/sd15-ddim" {
		t.Fatalf("expected contract_key on job record, got %v", jobPayload["contract_key"])
	}
	if jobPayload["workspace_name"] != "api-job-001" {
		t.Fatalf("expected workspace api-job-001, got %v", jobPayload["workspace_name"])
	}
}

func TestCreateJobRequiresContractKey(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	data, err := json.Marshal(map[string]any{
		"job_type":       "recon_artifact_mainline",
		"workspace_name": "api-job-001",
		"artifact_dir":   "D:/artifacts/recon-scores",
	})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/audit/jobs", bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", recorder.Code)
	}
	payload := decodeJSONResponse(t, recorder)
	if !strings.Contains(payload["detail"].(string), "contract_key") {
		t.Fatalf("expected contract_key validation error, got %v", payload["detail"])
	}
}

func TestCreateJobAcceptsGenericJobInputs(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	requestBody := map[string]any{
		"job_type":       "recon_artifact_mainline",
		"contract_key":   "black-box/recon/sd15-ddim",
		"workspace_name": "api-job-inputs-001",
		"job_inputs": map[string]any{
			"artifact_dir": "D:/artifacts/recon-scores",
			"method":       "quantile",
		},
	}
	data, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/audit/jobs", bytes.NewReader(data))
	createRequest.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(createRecorder, createRequest)

	if createRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", createRecorder.Code)
	}

	created := decodeJSONResponse(t, createRecorder)
	payload, ok := created["payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected payload object, got %T", created["payload"])
	}
	jobInputs, ok := payload["job_inputs"].(map[string]any)
	if !ok {
		t.Fatalf("expected job_inputs object, got %T", payload["job_inputs"])
	}
	if jobInputs["artifact_dir"] != "D:/artifacts/recon-scores" {
		t.Fatalf("expected artifact_dir in job_inputs, got %v", jobInputs["artifact_dir"])
	}
	if jobInputs["method"] != "quantile" {
		t.Fatalf("expected method in job_inputs, got %v", jobInputs["method"])
	}
}

func TestListJobsReturnsLatestFirst(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	createJob := func(workspaceName string, artifactDir string) {
		t.Helper()
		data, err := json.Marshal(map[string]any{
			"job_type":       "recon_artifact_mainline",
			"contract_key":   "black-box/recon/sd15-ddim",
			"workspace_name": workspaceName,
			"artifact_dir":   artifactDir,
		})
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/audit/jobs", bytes.NewReader(data))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d", recorder.Code)
		}
	}

	createJob("api-job-001", "D:/artifacts/one")
	createJob("api-job-002", "D:/artifacts/two")

	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit/jobs", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	var payload []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(payload) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(payload))
	}
	if payload[0]["workspace_name"] != "api-job-002" {
		t.Fatalf("expected latest job first, got %v", payload[0]["workspace_name"])
	}
}

func TestRejectsDuplicateActiveWorkspace(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	body := func(artifactDir string) *bytes.Reader {
		t.Helper()
		data, err := json.Marshal(map[string]any{
			"job_type":       "recon_artifact_mainline",
			"contract_key":   "black-box/recon/sd15-ddim",
			"workspace_name": "shared-workspace",
			"artifact_dir":   artifactDir,
		})
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}
		return bytes.NewReader(data)
	}

	firstRequest := httptest.NewRequest(http.MethodPost, "/api/v1/audit/jobs", body("D:/artifacts/one"))
	firstRequest.Header.Set("Content-Type", "application/json")
	firstRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(firstRecorder, firstRequest)
	if firstRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", firstRecorder.Code)
	}

	secondRequest := httptest.NewRequest(http.MethodPost, "/api/v1/audit/jobs", body("D:/artifacts/two"))
	secondRequest.Header.Set("Content-Type", "application/json")
	secondRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(secondRecorder, secondRequest)

	if secondRecorder.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", secondRecorder.Code)
	}
}

func TestBackgroundJobExecutionCompletesAndFillsSummary(t *testing.T) {
	root := t.TempDir()
	experimentsRoot := filepath.Join(root, "experiments")
	server := NewServer(Config{
		ExperimentsRoot: experimentsRoot,
		JobsRoot:        filepath.Join(root, "jobs"),
		AutoStartJobs:   true,
		Executor: func(payload auditJobCreate, workspacePath string) error {
			writeJSONFile(t, filepath.Join(workspacePath, "summary.json"), map[string]any{
				"status":    "ready",
				"paper":     "BlackBox_Reconstruction_ArXiv2023",
				"method":    "recon",
				"mode":      "artifact-mainline",
				"workspace": workspacePath,
				"metrics": map[string]any{
					"auc":             0.866,
					"asr":             0.51,
					"tpr_at_1pct_fpr": 1.0,
				},
				"artifact_paths": map[string]any{
					"summary": filepath.Join(workspacePath, "summary.json"),
				},
			})
			return nil
		},
	})

	data, err := json.Marshal(map[string]any{
		"job_type":       "recon_artifact_mainline",
		"contract_key":   "black-box/recon/sd15-ddim",
		"workspace_name": "api-job-complete",
		"artifact_dir":   "D:/artifacts/recon-scores",
	})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/audit/jobs", bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", recorder.Code)
	}

	created := decodeJSONResponse(t, recorder)
	jobID := created["job_id"].(string)

	record := waitForJobStatus(t, server, jobID, "completed")
	if record["summary_path"] == nil {
		t.Fatalf("expected summary_path to be filled")
	}
	metrics, ok := record["metrics"].(map[string]any)
	if !ok || metrics["auc"] != 0.866 {
		t.Fatalf("expected metrics to be filled, got %v", record["metrics"])
	}
}

func TestBackgroundJobExecutionPreservesNonReconMetrics(t *testing.T) {
	root := t.TempDir()
	experimentsRoot := filepath.Join(root, "experiments")
	server := NewServer(Config{
		ExperimentsRoot: experimentsRoot,
		JobsRoot:        filepath.Join(root, "jobs"),
		AutoStartJobs:   true,
		Executor: func(payload auditJobCreate, workspacePath string) error {
			writeJSONFile(t, filepath.Join(workspacePath, "summary.json"), map[string]any{
				"status":    "ready",
				"track":     "gray-box",
				"paper":     "GrayBox_PIA_Example",
				"method":    "pia",
				"mode":      "runtime-probe",
				"workspace": workspacePath,
				"metrics": map[string]any{
					"probe_auc":        0.73,
					"attack_accuracy":  0.69,
					"member_precision": 0.81,
				},
				"artifact_paths": map[string]any{
					"summary": filepath.Join(workspacePath, "summary.json"),
				},
			})
			return nil
		},
	})

	data, err := json.Marshal(map[string]any{
		"job_type":       "recon_artifact_mainline",
		"contract_key":   "black-box/recon/sd15-ddim",
		"workspace_name": "api-job-gray-metrics",
		"artifact_dir":   "D:/artifacts/recon-scores",
	})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/audit/jobs", bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", recorder.Code)
	}

	created := decodeJSONResponse(t, recorder)
	jobID := created["job_id"].(string)

	record := waitForJobStatus(t, server, jobID, "completed")
	metrics, ok := record["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("expected metrics object, got %T", record["metrics"])
	}
	if metrics["probe_auc"] != 0.73 {
		t.Fatalf("expected probe_auc to be preserved, got %v", metrics["probe_auc"])
	}
	if metrics["attack_accuracy"] != 0.69 {
		t.Fatalf("expected attack_accuracy to be preserved, got %v", metrics["attack_accuracy"])
	}
}

func TestBackgroundJobExecutionFailurePersistsError(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
		AutoStartJobs:   true,
		Executor: func(payload auditJobCreate, workspacePath string) error {
			return errors.New("stub execution failed")
		},
	})

	data, err := json.Marshal(map[string]any{
		"job_type":       "recon_artifact_mainline",
		"contract_key":   "black-box/recon/sd15-ddim",
		"workspace_name": "api-job-fail",
		"artifact_dir":   "D:/artifacts/recon-scores",
	})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/audit/jobs", bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", recorder.Code)
	}

	created := decodeJSONResponse(t, recorder)
	jobID := created["job_id"].(string)

	record := waitForJobStatus(t, server, jobID, "failed")
	if record["error"] == nil {
		t.Fatalf("expected error to be filled")
	}
}

func TestBackgroundJobFailureCapturesCommandAndOutputTail(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
		ProjectRoot:     root,
		RepoRoot:        "D:/repo",
		AutoStartJobs:   true,
		ExecCommand: func(command []string, dir string) ([]byte, error) {
			return []byte("line-1\nline-2\nline-3"), errors.New("process failed")
		},
	})

	data, err := json.Marshal(map[string]any{
		"job_type":       "recon_artifact_mainline",
		"contract_key":   "black-box/recon/sd15-ddim",
		"workspace_name": "api-job-fail-observable",
		"artifact_dir":   "D:/artifacts/recon-scores",
	})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/audit/jobs", bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", recorder.Code)
	}

	created := decodeJSONResponse(t, recorder)
	jobID := created["job_id"].(string)

	record := waitForJobStatus(t, server, jobID, "failed")
	command, ok := record["command"].([]any)
	if !ok || len(command) == 0 {
		t.Fatalf("expected command to be captured, got %v", record["command"])
	}
	stderrTail, ok := record["stderr_tail"].([]any)
	if !ok || len(stderrTail) == 0 {
		t.Fatalf("expected stderr_tail to be captured, got %v", record["stderr_tail"])
	}
	if stderrTail[len(stderrTail)-1] != "line-3" {
		t.Fatalf("expected last stderr tail line line-3, got %v", stderrTail)
	}
}

func waitForJobStatus(t *testing.T, server *Server, jobID string, expected string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/audit/jobs/"+jobID, nil)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", recorder.Code)
		}
		payload := decodeJSONResponse(t, recorder)
		if payload["status"] == expected {
			return payload
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach status %s", jobID, expected)
	return nil
}
