package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestShouldRequestGPU(t *testing.T) {
	if shouldRequestGPU(auditJobCreate{JobType: "recon_artifact_mainline"}) {
		t.Fatalf("recon_artifact_mainline should not request gpu")
	}
	if !shouldRequestGPU(auditJobCreate{JobType: "recon_runtime_mainline"}) {
		t.Fatalf("recon_runtime_mainline should request gpu")
	}
}

func TestExecutePythonJobAcquiresAndReleasesGPU(t *testing.T) {
	root := t.TempDir()
	acquired := 0
	released := 0
	executed := 0

	server := NewServer(Config{
		ProjectRoot: root,
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
	if jobPayload["workspace_name"] != "api-job-001" {
		t.Fatalf("expected workspace api-job-001, got %v", jobPayload["workspace_name"])
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
