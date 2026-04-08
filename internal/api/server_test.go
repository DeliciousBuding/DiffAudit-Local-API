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
	if definition, ok := contractDefinitionByKey(contractKey); ok {
		return definition
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

func TestDiagnosticsEndpointReportsPathsAndConfig(t *testing.T) {
	root := t.TempDir()
	serviceRoot := filepath.Join(root, "service")
	runnersRoot := filepath.Join(serviceRoot, "runners")
	registryDBPath := filepath.Join(serviceRoot, "config", "registry.db")
	experiments := filepath.Join(root, "experiments")
	jobs := filepath.Join(root, "jobs")
	project := filepath.Join(root, "project")
	repo := filepath.Join(root, "repo")
	for _, path := range []string{
		experiments,
		jobs,
		project,
		repo,
		filepath.Join(runnersRoot, "recon-runner"),
		filepath.Join(runnersRoot, "pia-runner"),
		filepath.Join(runnersRoot, "gsa-runner"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
	}
	for _, file := range []string{
		filepath.Join(runnersRoot, "recon-runner", "run.py"),
		filepath.Join(runnersRoot, "recon-runner", "Dockerfile"),
		filepath.Join(runnersRoot, "pia-runner", "run.py"),
		filepath.Join(runnersRoot, "pia-runner", "Dockerfile"),
		filepath.Join(runnersRoot, "gsa-runner", "run.py"),
		filepath.Join(runnersRoot, "gsa-runner", "Dockerfile"),
	} {
		if err := os.WriteFile(file, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}
	server := NewServer(Config{
		ServiceRoot:      serviceRoot,
		RegistryDBPath:   registryDBPath,
		RunnersRoot:      runnersRoot,
		ExperimentsRoot:  experiments,
		JobsRoot:         jobs,
		ProjectRoot:      project,
		RepoRoot:         repo,
		ExecutionMode:    "docker",
		DockerBinary:     "docker",
		GPUSchedulerPath: "/opt/gpu-scheduler",
		GPURequestDoc:    "docs/gpu-request.md",
	})
	t.Cleanup(func() {
		if server.registry != nil && server.registry.db != nil {
			_ = server.registry.db.Close()
		}
	})

	request := httptest.NewRequest(http.MethodGet, "/diagnostics", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	payload := decodeJSONResponse(t, recorder)
	if payload["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", payload["status"])
	}
	if payload["execution_mode"] != "docker" {
		t.Fatalf("expected execution_mode docker, got %v", payload["execution_mode"])
	}
	paths, ok := payload["paths"].(map[string]any)
	if !ok {
		t.Fatalf("expected paths map, got %T", payload["paths"])
	}
	checkPath := func(key, want string) {
		entry, ok := paths[key].(map[string]any)
		if !ok {
			t.Fatalf("expected %s to be map, got %T", key, paths[key])
		}
		if entry["path"] != want {
			t.Fatalf("expected %s path %s, got %v", key, want, entry["path"])
		}
		if exists, ok := entry["exists"].(bool); !ok || !exists {
			t.Fatalf("expected %s exists=true, got %v", key, entry["exists"])
		}
	}
	checkPath("experiments_root", experiments)
	checkPath("jobs_root", jobs)
	checkPath("project_root", project)
	checkPath("repo_root", repo)
	checkPath("service_root", serviceRoot)
	checkPath("registry_db_path", registryDBPath)
	checkPath("runners_root", runnersRoot)

	runners, ok := payload["runners"].(map[string]any)
	if !ok {
		t.Fatalf("expected runners map, got %T", payload["runners"])
	}
	recon, ok := runners["recon"].(map[string]any)
	if !ok {
		t.Fatalf("expected recon runner map, got %T", runners["recon"])
	}
	script, ok := recon["script"].(map[string]any)
	if !ok || script["exists"] != true {
		t.Fatalf("expected recon runner script to exist, got %v", recon["script"])
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
		ServiceRoot: root,
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
			JobInputs: map[string]any{
				"target_member_dataset":    "D:/datasets/target_member.pt",
				"target_nonmember_dataset": "D:/datasets/target_non_member.pt",
				"shadow_member_dataset":    "D:/datasets/shadow_member.pt",
				"shadow_nonmember_dataset": "D:/datasets/shadow_non_member.pt",
				"target_model_dir":         "D:/models/target",
				"shadow_model_dir":         "D:/models/shadow",
			},
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
	first := payload[0]
	if first["contract_key"] == nil {
		t.Fatalf("expected models endpoint to expose contract_key, got %v", first)
	}
	if first["attack_family"] == nil {
		t.Fatalf("expected models endpoint to expose attack_family, got %v", first)
	}
	if first["contract_status"] == nil {
		t.Fatalf("expected models endpoint to expose contract_status, got %v", first)
	}
	if first["track"] == nil {
		t.Fatalf("expected models endpoint to expose track, got %v", first)
	}
	if first["target_key"] == nil {
		t.Fatalf("expected models endpoint to expose target_key, got %v", first)
	}
}

func TestContractRegistryIncludesTargetGrayAndWhiteContracts(t *testing.T) {
	pia := findContractDefinition(t, "gray-box/pia/cifar10-ddpm")
	if pia.ContractStatus != "live" {
		t.Fatalf("expected pia contract_status live, got %s", pia.ContractStatus)
	}
	if !pia.CatalogVisible {
		t.Fatalf("expected pia contract to be visible in live catalog")
	}
	if len(pia.Jobs) == 0 {
		t.Fatal("expected pia contract to expose at least one live job")
	}
	if pia.FeatureAccess != "epsilon_t" {
		t.Fatalf("expected pia feature_access epsilon_t, got %s", pia.FeatureAccess)
	}
	if len(pia.LivePromotionGates) == 0 {
		t.Fatal("expected pia live promotion gates")
	}

	gsa := findContractDefinition(t, "white-box/gsa/ddpm-cifar10")
	if gsa.ContractStatus != "live" {
		t.Fatalf("expected gsa contract_status live, got %s", gsa.ContractStatus)
	}
	if !gsa.CatalogVisible {
		t.Fatalf("expected gsa contract to be visible in live catalog")
	}
	if len(gsa.Jobs) == 0 {
		t.Fatal("expected gsa contract to expose at least one live job")
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
	if len(payload) < 4 {
		t.Fatalf("expected at least 4 catalog entries, got %d", len(payload))
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
	if entry["label"] != "Stable Diffusion 1.5 + DDIM" {
		t.Fatalf("expected label from shared contract projection, got %v", entry["label"])
	}
}

func TestContractProjectionKeepsModelsAndCatalogAligned(t *testing.T) {
	models := liveModelOptions()
	if len(models) == 0 {
		t.Fatal("expected at least one live model option")
	}
	model := models[0]
	definition, ok := contractDefinitionByKey(model.ContractKey)
	if !ok {
		t.Fatalf("missing contract definition for %s", model.ContractKey)
	}
	catalogEntry := projectCatalogEntry(definition)
	if model.Track != catalogEntry.Track {
		t.Fatalf("expected track alignment, got model=%s catalog=%s", model.Track, catalogEntry.Track)
	}
	if model.AttackFamily != catalogEntry.AttackFamily {
		t.Fatalf("expected attack family alignment, got model=%s catalog=%s", model.AttackFamily, catalogEntry.AttackFamily)
	}
	if model.TargetKey != catalogEntry.TargetKey {
		t.Fatalf("expected target key alignment, got model=%s catalog=%s", model.TargetKey, catalogEntry.TargetKey)
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

func TestCatalogEndpointHydratesIntakeMetadataWhenAvailable(t *testing.T) {
	root := t.TempDir()
	projectRoot := filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(projectRoot, "workspaces", "intake"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(projectRoot, "workspaces", "intake", "index.json"),
		[]byte(`{
  "schema": "diffaudit.intake.index.v1",
  "updated_at": "2026-04-07",
  "entries": [
    {
      "id": "gray-box/pia/cifar10-ddpm",
      "contract_key": "gray-box/pia/cifar10-ddpm",
      "track": "gray-box",
      "method": "pia",
      "manifest": "workspaces/gray-box/assets/pia/manifest.json",
      "admission": {
        "status": "admitted",
        "level": "system-intake-ready",
        "evidence_level": "runtime-mainline",
        "provenance_status": "workspace-verified"
      },
      "compatibility": {
        "surface": "diffaudit-cli",
        "commands": [
          {
            "name": "run-pia-runtime-mainline",
            "required_manifest_fields": ["checkpoint_root"]
          }
        ]
      }
    }
  ]
}`),
		0o644,
	); err != nil {
		t.Fatalf("write intake index failed: %v", err)
	}

	server := NewServer(Config{
		ProjectRoot: projectRoot,
		JobsRoot:    filepath.Join(root, "jobs"),
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	payload := decodeJSONArrayResponse(t, recorder)
	entry := findCatalogEntry(t, payload, "pia", "cifar10-ddpm")
	if entry["admission_status"] != "admitted" {
		t.Fatalf("expected admission_status admitted, got %v", entry["admission_status"])
	}
	if entry["admission_level"] != "system-intake-ready" {
		t.Fatalf("expected admission_level system-intake-ready, got %v", entry["admission_level"])
	}
	if entry["provenance_status"] != "workspace-verified" {
		t.Fatalf("expected provenance_status workspace-verified, got %v", entry["provenance_status"])
	}
	if entry["intake_manifest"] != "workspaces/gray-box/assets/pia/manifest.json" {
		t.Fatalf("expected intake_manifest path, got %v", entry["intake_manifest"])
	}
}

func TestUnifiedAttackDefenseTableEndpoint(t *testing.T) {
	root := t.TempDir()
	projectRoot := filepath.Join(root, "project")
	tablePath := filepath.Join(projectRoot, "workspaces", "implementation", "artifacts", "unified-attack-defense-table.json")
	writeJSONFile(t, tablePath, map[string]any{
		"schema": "diffaudit.attack_defense_table.v1",
		"rows": []map[string]any{
			{
				"track":   "gray-box",
				"attack":  "PIA GPU512 baseline",
				"defense": "G-1 stochastic-dropout",
			},
		},
	})

	server := NewServer(Config{
		ProjectRoot: projectRoot,
		JobsRoot:    filepath.Join(root, "jobs"),
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/evidence/attack-defense-table", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	payload := decodeJSONResponse(t, recorder)
	if payload["schema"] != "diffaudit.attack_defense_table.v1" {
		t.Fatalf("expected unified table schema, got %v", payload["schema"])
	}
	rows, ok := payload["rows"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("expected one row, got %v", payload["rows"])
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
	if payload["contract_key"] != "black-box/recon/sd15-ddim" {
		t.Fatalf("expected contract_key black-box/recon/sd15-ddim, got %v", payload["contract_key"])
	}
	if payload["attack_family"] != "recon" {
		t.Fatalf("expected attack_family recon, got %v", payload["attack_family"])
	}
	if payload["target_key"] != "sd15-ddim" {
		t.Fatalf("expected target_key sd15-ddim, got %v", payload["target_key"])
	}
	metrics, ok := payload["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("expected metrics object, got %T", payload["metrics"])
	}
	if metrics["tpr_at_1pct_fpr"] != 1.0 {
		t.Fatalf("expected tpr_at_1pct_fpr 1.0, got %v", metrics["tpr_at_1pct_fpr"])
	}
}

func TestWorkspaceSummaryInfersTrackFromRegistryContract(t *testing.T) {
	root := t.TempDir()
	experimentsRoot := filepath.Join(root, "experiments")
	workspace := filepath.Join(experimentsRoot, "pia-runtime-probe-001")

	writeJSONFile(t, filepath.Join(workspace, "summary.json"), map[string]any{
		"status":    "ready",
		"paper":     "PIA",
		"method":    "pia",
		"mode":      "runtime-probe",
		"workspace": workspace,
		"runtime": map[string]any{
			"backend": "ddpm",
		},
		"metrics": map[string]any{
			"probe_auc": 0.73,
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
		"/api/v1/experiments/pia-runtime-probe-001/summary",
		nil,
	)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	payload := decodeJSONResponse(t, recorder)
	if payload["track"] != "gray-box" {
		t.Fatalf("expected inferred gray-box track, got %v", payload["track"])
	}
	if payload["contract_key"] != "gray-box/pia/cifar10-ddpm" {
		t.Fatalf("expected gray-box contract_key, got %v", payload["contract_key"])
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

func TestCreateJobAcceptsPiaRuntimeMainline(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	requestBody := map[string]any{
		"job_type":       "pia_runtime_mainline",
		"contract_key":   "gray-box/pia/cifar10-ddpm",
		"workspace_name": "api-pia-001",
		"repo_root":      "D:/Code/DiffAudit/Project/external/PIA",
		"job_inputs": map[string]any{
			"config":            "D:/Code/DiffAudit/Project/tmp/configs/pia-cifar10-graybox-assets.local.yaml",
			"member_split_root": "D:/Code/DiffAudit/Project/external/PIA/DDPM",
			"device":            "cpu",
		},
	}
	data, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/audit/jobs", bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateJobAcceptsGsaRuntimeMainline(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	requestBody := map[string]any{
		"job_type":       "gsa_runtime_mainline",
		"contract_key":   "white-box/gsa/ddpm-cifar10",
		"workspace_name": "api-gsa-001",
		"repo_root":      "D:/Code/DiffAudit/Project/workspaces/white-box/external/GSA",
		"job_inputs": map[string]any{
			"assets_root":        "D:/Code/DiffAudit/Project/workspaces/white-box/assets/gsa",
			"resolution":         "32",
			"ddpm_num_steps":     "20",
			"sampling_frequency": "2",
			"attack_method":      "1",
		},
	}
	data, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/audit/jobs", bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateJobAcceptsRuntimeProfileAndAssets(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	requestBody := map[string]any{
		"job_type":        "gsa_runtime_mainline",
		"contract_key":    "white-box/gsa/ddpm-cifar10",
		"workspace_name":  "api-gsa-docker-001",
		"runtime_profile": "docker-default",
		"repo_root":       "D:/Code/DiffAudit/Project/workspaces/white-box/external/GSA",
		"assets": map[string]any{
			"assets_root": "D:/Code/DiffAudit/Project/workspaces/white-box/assets/gsa",
		},
		"job_inputs": map[string]any{
			"resolution":         "32",
			"ddpm_num_steps":     "20",
			"sampling_frequency": "2",
			"attack_method":      "1",
		},
	}
	data, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/audit/jobs", bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	payload := decodeJSONResponse(t, recorder)
	created, ok := payload["payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected payload object, got %T", payload["payload"])
	}
	if created["runtime_profile"] != "docker-default" {
		t.Fatalf("expected runtime_profile docker-default, got %v", created["runtime_profile"])
	}
	jobInputs, ok := created["job_inputs"].(map[string]any)
	if !ok {
		t.Fatalf("expected job_inputs object, got %T", created["job_inputs"])
	}
	if jobInputs["assets_root"] != "D:/Code/DiffAudit/Project/workspaces/white-box/assets/gsa" {
		t.Fatalf("expected assets_root in normalized job_inputs, got %v", jobInputs["assets_root"])
	}
}

func TestCreateJobRejectsUnknownRuntimeProfile(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	requestBody := map[string]any{
		"job_type":        "pia_runtime_mainline",
		"contract_key":    "gray-box/pia/cifar10-ddpm",
		"workspace_name":  "api-pia-invalid-runtime-001",
		"runtime_profile": "k8s-default",
		"repo_root":       "D:/Code/DiffAudit/Project/external/PIA",
		"job_inputs": map[string]any{
			"config": "D:/Code/DiffAudit/Project/tmp/configs/pia-cifar10-graybox-assets.local.yaml",
		},
	}
	data, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/audit/jobs", bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateJobAcceptsRuntimeProfileForPiaRuntimeMainline(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	requestBody := map[string]any{
		"job_type":        "pia_runtime_mainline",
		"contract_key":    "gray-box/pia/cifar10-ddpm",
		"workspace_name":  "api-pia-runtime-profile-001",
		"repo_root":       "D:/Code/DiffAudit/Project/external/PIA",
		"runtime_profile": "local-default",
		"assets": map[string]any{
			"member_split_root": "D:/Code/DiffAudit/Project/external/PIA/DDPM",
		},
		"job_inputs": map[string]any{
			"config": "D:/Code/DiffAudit/Project/tmp/configs/pia-cifar10-graybox-assets.local.yaml",
			"device": "cpu",
		},
	}
	data, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/audit/jobs", bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	created := decodeJSONResponse(t, recorder)
	payload, ok := created["payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected payload object, got %T", created["payload"])
	}
	jobInputs, ok := payload["job_inputs"].(map[string]any)
	if !ok {
		t.Fatalf("expected job_inputs object, got %T", payload["job_inputs"])
	}
	if payload["runtime_profile"] != "local-default" {
		t.Fatalf("expected runtime_profile preserved, got %v", payload["runtime_profile"])
	}
	if jobInputs["config"] != requestBody["job_inputs"].(map[string]any)["config"] {
		t.Fatalf("expected config propagated, got %v", jobInputs["config"])
	}
	if jobInputs["member_split_root"] != requestBody["assets"].(map[string]any)["member_split_root"] {
		t.Fatalf("expected member_split_root propagated, got %v", jobInputs["member_split_root"])
	}
}

func TestCreateJobAcceptsAssetsFieldForGsaRuntimeMainline(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	requestBody := map[string]any{
		"job_type":       "gsa_runtime_mainline",
		"contract_key":   "white-box/gsa/ddpm-cifar10",
		"workspace_name": "api-gsa-assets-001",
		"repo_root":      "D:/Code/DiffAudit/Project/workspaces/white-box/external/GSA",
		"assets": map[string]any{
			"assets_root": "D:/Code/DiffAudit/Project/workspaces/white-box/assets/gsa",
			"resolution":  "40",
		},
	}
	data, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/audit/jobs", bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	created := decodeJSONResponse(t, recorder)
	payload, ok := created["payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected payload object, got %T", created["payload"])
	}
	jobInputs, ok := payload["job_inputs"].(map[string]any)
	if !ok {
		t.Fatalf("expected job_inputs object, got %T", payload["job_inputs"])
	}
	if jobInputs["assets_root"] != requestBody["assets"].(map[string]any)["assets_root"] {
		t.Fatalf("expected assets_root propagated, got %v", jobInputs["assets_root"])
	}
	if jobInputs["resolution"] != requestBody["assets"].(map[string]any)["resolution"] {
		t.Fatalf("expected resolution propagated, got %v", jobInputs["resolution"])
	}
}

func TestPlannedJobCommandIncludesRuntimeProfileForPia(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ProjectRoot:     "D:/Code/DiffAudit/Project",
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	command, err := server.plannedJobCommand(
		auditJobCreate{
			JobType:        "pia_runtime_mainline",
			ContractKey:    "gray-box/pia/cifar10-ddpm",
			WorkspaceName:  "planned-pia-runtime-profile",
			RepoRoot:       "D:/Code/DiffAudit/Project/external/PIA",
			RuntimeProfile: "local-default",
			Assets: map[string]any{
				"member_split_root": "D:/Code/DiffAudit/Project/external/PIA/DDPM",
			},
			JobInputs: map[string]any{
				"config":      "D:/Code/DiffAudit/Project/tmp/configs/pia-cifar10-graybox-assets.local.yaml",
				"num_samples": "32",
			},
		},
		filepath.Join(root, "experiments", "planned-pia-runtime-profile"),
	)
	if err != nil {
		t.Fatalf("plannedJobCommand returned error: %v", err)
	}
	commandLine := strings.Join(command, "\n")
	for _, want := range []string{
		"--config\nD:/Code/DiffAudit/Project/tmp/configs/pia-cifar10-graybox-assets.local.yaml",
		"--member-split-root\nD:/Code/DiffAudit/Project/external/PIA/DDPM",
		"--max-samples\n32",
	} {
		if !strings.Contains(commandLine, want) {
			t.Fatalf("expected pia command to contain %q, got %v", want, command)
		}
	}
}

func TestPlannedJobCommandIncludesAssetsForGsa(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ProjectRoot:     "D:/Code/DiffAudit/Project",
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
	})

	command, err := server.plannedJobCommand(
		auditJobCreate{
			JobType:        "gsa_runtime_mainline",
			ContractKey:    "white-box/gsa/ddpm-cifar10",
			WorkspaceName:  "planned-gsa-runtime-profile",
			RepoRoot:       "D:/Code/DiffAudit/Project/workspaces/white-box/external/GSA",
			RuntimeProfile: "local-default",
			Assets: map[string]any{
				"assets_root": "D:/Code/DiffAudit/Project/workspaces/white-box/assets/gsa",
			},
			JobInputs: map[string]any{
				"resolution":    "64",
				"attack_method": "3",
			},
		},
		filepath.Join(root, "experiments", "planned-gsa-runtime-profile"),
	)
	if err != nil {
		t.Fatalf("plannedJobCommand returned error: %v", err)
	}
	commandLine := strings.Join(command, "\n")
	for _, want := range []string{
		"--assets-root\nD:/Code/DiffAudit/Project/workspaces/white-box/assets/gsa",
		"--resolution\n64",
		"--attack-method\n3",
	} {
		if !strings.Contains(commandLine, want) {
			t.Fatalf("expected gsa command to contain %q, got %v", want, command)
		}
	}
}
func TestPlannedJobCommandUsesDockerExecutorWhenConfigured(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ProjectRoot:     "D:/Code/DiffAudit/Project",
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
		ExecutionMode:   "docker",
		DockerBinary:    "docker",
	})

	command, err := server.plannedJobCommand(
		auditJobCreate{
			JobType:       "pia_runtime_mainline",
			ContractKey:   "gray-box/pia/cifar10-ddpm",
			WorkspaceName: "docker-pia-001",
			RepoRoot:      "D:/Code/DiffAudit/Project/external/PIA",
			JobInputs: map[string]any{
				"config":            "D:/Code/DiffAudit/Project/tmp/configs/pia-cifar10-graybox-assets.local.yaml",
				"member_split_root": "D:/Code/DiffAudit/Project/external/PIA/DDPM",
			},
		},
		"D:/Code/DiffAudit/Project/experiments/docker-pia-001",
	)
	if err != nil {
		t.Fatalf("plannedJobCommand returned error: %v", err)
	}
	commandLine := strings.Join(command, "\n")
	for _, want := range []string{
		"docker",
		"diffaudit/pia-runner:latest",
		"run-pia-runtime-mainline",
		"/job/output",
	} {
		if !strings.Contains(commandLine, want) {
			t.Fatalf("expected docker command to contain %q, got %v", want, command)
		}
	}
}

func TestPlannedJobCommandUsesPayloadRuntimeProfile(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		ProjectRoot:     "D:/Code/DiffAudit/Project",
		ExperimentsRoot: filepath.Join(root, "experiments"),
		JobsRoot:        filepath.Join(root, "jobs"),
		ExecutionMode:   "local",
		DockerBinary:    "docker",
	})

	command, err := server.plannedJobCommand(
		auditJobCreate{
			JobType:        "gsa_runtime_mainline",
			ContractKey:    "white-box/gsa/ddpm-cifar10",
			WorkspaceName:  "docker-gsa-001",
			RuntimeProfile: "docker-default",
			RepoRoot:       "D:/Code/DiffAudit/Project/workspaces/white-box/external/GSA",
			Assets: map[string]any{
				"assets_root": "D:/Code/DiffAudit/Project/workspaces/white-box/assets/gsa",
			},
			JobInputs: map[string]any{
				"resolution": "32",
			},
		},
		"D:/Code/DiffAudit/Project/experiments/docker-gsa-001",
	)
	if err != nil {
		t.Fatalf("plannedJobCommand returned error: %v", err)
	}
	commandLine := strings.Join(command, "\n")
	for _, want := range []string{
		"docker",
		"diffaudit/gsa-runner:latest",
		"run-gsa-runtime-mainline",
		"/workspace/project/workspaces/white-box/assets/gsa",
	} {
		if !strings.Contains(commandLine, want) {
			t.Fatalf("expected payload-selected docker command to contain %q, got %v", want, command)
		}
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
	if record["output_capture"] != "combined" {
		t.Fatalf("expected combined output_capture, got %v", record["output_capture"])
	}
	outputTail, ok := record["output_tail"].([]any)
	if !ok || len(outputTail) == 0 {
		t.Fatalf("expected output_tail to be captured, got %v", record["output_tail"])
	}
	if outputTail[len(outputTail)-1] != "line-3" {
		t.Fatalf("expected last output tail line line-3, got %v", outputTail)
	}
	if record["stderr_tail"] != nil {
		t.Fatalf("expected stderr_tail to stay nil for combined capture, got %v", record["stderr_tail"])
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
