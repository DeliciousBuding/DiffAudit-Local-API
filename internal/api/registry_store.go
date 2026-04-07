package api

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed registry_seed.json
var registrySeedJSON []byte

const registrySchemaSQL = `
CREATE TABLE IF NOT EXISTS contracts (
  contract_key TEXT PRIMARY KEY,
  track TEXT NOT NULL,
  attack_family TEXT NOT NULL,
  target_key TEXT NOT NULL,
  label TEXT NOT NULL,
  paper TEXT NOT NULL,
  backend TEXT NOT NULL,
  scheduler TEXT,
  availability TEXT NOT NULL,
  default_evidence_level TEXT NOT NULL,
  contract_status TEXT NOT NULL,
  registry_evidence TEXT NOT NULL,
  feature_access TEXT NOT NULL,
  checkpoint_format TEXT NOT NULL,
  required_inputs_now_json TEXT NOT NULL,
  optional_inputs_later_json TEXT NOT NULL,
  promoted_asset_roots_json TEXT NOT NULL,
  live_promotion_gates_json TEXT NOT NULL,
  system_gap TEXT NOT NULL,
  catalog_visible INTEGER NOT NULL,
  status_method_key TEXT NOT NULL,
  model_json TEXT
);

CREATE TABLE IF NOT EXISTS jobs (
  contract_key TEXT NOT NULL,
  job_type TEXT NOT NULL,
  runner TEXT NOT NULL,
  required_inputs_json TEXT NOT NULL,
  requests_gpu INTEGER NOT NULL,
  PRIMARY KEY (contract_key, job_type)
);

CREATE TABLE IF NOT EXISTS assets (
  asset_key TEXT PRIMARY KEY,
  scope_root TEXT NOT NULL,
  path TEXT NOT NULL,
  description TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
`

type registryStore struct {
	db *sql.DB
}

type assetRecord struct {
	AssetKey    string
	ScopeRoot   string
	Path        string
	Description string
	CreatedAt   string
	UpdatedAt   string
}

type assetResolveContext struct {
	ProjectRoot string
	ServiceRoot string
	RepoRoot    string
}

var (
	defaultRegistryOnce sync.Once
	defaultRegistry     *registryStore
	defaultRegistryErr  error
)

func defaultRegistryStore() (*registryStore, error) {
	defaultRegistryOnce.Do(func() {
		defaultRegistry, defaultRegistryErr = openRegistryStore("")
	})
	return defaultRegistry, defaultRegistryErr
}

func openRegistryStore(path string) (*registryStore, error) {
	dsn := path
	if strings.TrimSpace(dsn) == "" {
		dsn = "file:diffaudit-registry?mode=memory&cache=shared"
	} else {
		if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(registrySchemaSQL); err != nil {
		_ = db.Close()
		return nil, err
	}
	store := &registryStore{db: db}
	if err := store.seedIfEmpty(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *registryStore) UpsertAsset(assetKey string, scopeRoot string, assetPath string, description string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	assetKey = strings.TrimSpace(assetKey)
	scopeRoot = strings.TrimSpace(scopeRoot)
	assetPath = strings.TrimSpace(assetPath)
	description = strings.TrimSpace(description)
	if assetKey == "" {
		return fmt.Errorf("asset_key is required")
	}
	if scopeRoot == "" {
		return fmt.Errorf("scope_root is required")
	}
	if assetPath == "" {
		return fmt.Errorf("path is required")
	}
	switch scopeRoot {
	case "absolute", "project_root", "service_root", "repo_root":
	default:
		return fmt.Errorf("unsupported scope_root %q", scopeRoot)
	}

	_, err := s.db.Exec(
		`INSERT INTO assets (asset_key, scope_root, path, description, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(asset_key) DO UPDATE SET
		   scope_root=excluded.scope_root,
		   path=excluded.path,
		   description=excluded.description,
		   updated_at=excluded.updated_at`,
		assetKey,
		scopeRoot,
		assetPath,
		description,
		now,
		now,
	)
	return err
}

func (s *registryStore) assetByKey(assetKey string) (assetRecord, bool, error) {
	var asset assetRecord
	err := s.db.QueryRow(
		`SELECT asset_key, scope_root, path, description, created_at, updated_at
		 FROM assets WHERE asset_key = ?`,
		strings.TrimSpace(assetKey),
	).Scan(&asset.AssetKey, &asset.ScopeRoot, &asset.Path, &asset.Description, &asset.CreatedAt, &asset.UpdatedAt)
	if err == sql.ErrNoRows {
		return assetRecord{}, false, nil
	}
	if err != nil {
		return assetRecord{}, false, err
	}
	return asset, true, nil
}

func resolveAssetRefValue(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "asset://") {
		return "", false
	}
	key := strings.TrimSpace(strings.TrimPrefix(value, "asset://"))
	if key == "" {
		return "", true
	}
	return key, true
}

func (s *registryStore) ResolveAssetRef(ref string, ctx assetResolveContext) (string, error) {
	key, ok := resolveAssetRefValue(ref)
	if !ok {
		return strings.TrimSpace(ref), nil
	}
	if key == "" {
		return "", fmt.Errorf("invalid asset ref %q", ref)
	}
	asset, found, err := s.assetByKey(key)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("unknown asset %q", key)
	}

	switch asset.ScopeRoot {
	case "absolute":
		return filepath.Clean(filepath.FromSlash(asset.Path)), nil
	case "project_root":
		if strings.TrimSpace(ctx.ProjectRoot) == "" {
			return "", fmt.Errorf("project_root is required to resolve asset %q", key)
		}
		return filepath.Clean(filepath.Join(ctx.ProjectRoot, filepath.FromSlash(asset.Path))), nil
	case "service_root":
		if strings.TrimSpace(ctx.ServiceRoot) == "" {
			return "", fmt.Errorf("service_root is required to resolve asset %q", key)
		}
		return filepath.Clean(filepath.Join(ctx.ServiceRoot, filepath.FromSlash(asset.Path))), nil
	case "repo_root":
		if strings.TrimSpace(ctx.RepoRoot) == "" {
			return "", fmt.Errorf("repo_root is required to resolve asset %q", key)
		}
		return filepath.Clean(filepath.Join(ctx.RepoRoot, filepath.FromSlash(asset.Path))), nil
	default:
		return "", fmt.Errorf("unsupported scope_root %q for asset %q", asset.ScopeRoot, key)
	}
}

func (s *registryStore) ResolveAssetRefsInInputs(inputs map[string]any, ctx assetResolveContext) error {
	if len(inputs) == 0 {
		return nil
	}
	for key, value := range inputs {
		str, ok := value.(string)
		if !ok {
			continue
		}
		assetKey, isRef := resolveAssetRefValue(str)
		if !isRef {
			continue
		}
		if assetKey == "" {
			return fmt.Errorf("invalid asset ref for %s", key)
		}
		resolved, err := s.ResolveAssetRef(str, ctx)
		if err != nil {
			return err
		}
		inputs[key] = resolved
	}
	return nil
}

func (s *registryStore) seedIfEmpty() error {
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM contracts").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	var seed []contractDefinition
	if err := json.Unmarshal(registrySeedJSON, &seed); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, definition := range seed {
		if err := insertContract(tx, definition); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func insertContract(tx *sql.Tx, definition contractDefinition) error {
	requiredNow, _ := json.Marshal(definition.RequiredInputsNow)
	optionalLater, _ := json.Marshal(definition.OptionalInputsLater)
	promotedRoots, _ := json.Marshal(definition.PromotedAssetRoots)
	promotionGates, _ := json.Marshal(definition.LivePromotionGates)
	modelPayload, _ := json.Marshal(definition.Model)
	if _, err := tx.Exec(`
INSERT INTO contracts (
  contract_key, track, attack_family, target_key, label, paper, backend, scheduler,
  availability, default_evidence_level, contract_status, registry_evidence,
  feature_access, checkpoint_format, required_inputs_now_json, optional_inputs_later_json,
  promoted_asset_roots_json, live_promotion_gates_json, system_gap, catalog_visible,
  status_method_key, model_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(contract_key) DO UPDATE SET
  track=excluded.track,
  attack_family=excluded.attack_family,
  target_key=excluded.target_key,
  label=excluded.label,
  paper=excluded.paper,
  backend=excluded.backend,
  scheduler=excluded.scheduler,
  availability=excluded.availability,
  default_evidence_level=excluded.default_evidence_level,
  contract_status=excluded.contract_status,
  registry_evidence=excluded.registry_evidence,
  feature_access=excluded.feature_access,
  checkpoint_format=excluded.checkpoint_format,
  required_inputs_now_json=excluded.required_inputs_now_json,
  optional_inputs_later_json=excluded.optional_inputs_later_json,
  promoted_asset_roots_json=excluded.promoted_asset_roots_json,
  live_promotion_gates_json=excluded.live_promotion_gates_json,
  system_gap=excluded.system_gap,
  catalog_visible=excluded.catalog_visible,
  status_method_key=excluded.status_method_key,
  model_json=excluded.model_json`,
		definition.ContractKey,
		definition.Track,
		definition.AttackFamily,
		definition.TargetKey,
		definition.Label,
		definition.Paper,
		definition.Backend,
		definition.Scheduler,
		definition.Availability,
		definition.DefaultEvidenceLevel,
		definition.ContractStatus,
		definition.RegistryEvidence,
		definition.FeatureAccess,
		definition.CheckpointFormat,
		string(requiredNow),
		string(optionalLater),
		string(promotedRoots),
		string(promotionGates),
		definition.SystemGap,
		boolToInt(definition.CatalogVisible),
		definition.StatusMethodKey,
		string(modelPayload),
	); err != nil {
		return err
	}
	for _, job := range definition.Jobs {
		requiredInputs, _ := json.Marshal(job.RequiredInputs)
		if _, err := tx.Exec(
			`INSERT INTO jobs (contract_key, job_type, runner, required_inputs_json, requests_gpu) VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(contract_key, job_type) DO UPDATE SET
			   runner=excluded.runner,
			   required_inputs_json=excluded.required_inputs_json,
			   requests_gpu=excluded.requests_gpu`,
			definition.ContractKey,
			job.JobType,
			job.Runner,
			string(requiredInputs),
			boolToInt(job.RequestsGPU),
		); err != nil {
			return err
		}
	}
	return nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func intToBool(value int) bool {
	return value != 0
}

func (s *registryStore) Contracts() ([]contractDefinition, error) {
	rows, err := s.db.Query(`
SELECT contract_key, track, attack_family, target_key, label, paper, backend, scheduler,
       availability, default_evidence_level, contract_status, registry_evidence,
       feature_access, checkpoint_format, required_inputs_now_json, optional_inputs_later_json,
       promoted_asset_roots_json, live_promotion_gates_json, system_gap, catalog_visible,
       status_method_key, model_json
FROM contracts ORDER BY contract_key`)
	if err != nil {
		return nil, err
	}
	definitions := make([]contractDefinition, 0)
	for rows.Next() {
		var definition contractDefinition
		var scheduler sql.NullString
		var requiredNow, optionalLater, promotedRoots, promotionGates, modelJSON string
		var catalogVisible int
		if err := rows.Scan(
			&definition.ContractKey,
			&definition.Track,
			&definition.AttackFamily,
			&definition.TargetKey,
			&definition.Label,
			&definition.Paper,
			&definition.Backend,
			&scheduler,
			&definition.Availability,
			&definition.DefaultEvidenceLevel,
			&definition.ContractStatus,
			&definition.RegistryEvidence,
			&definition.FeatureAccess,
			&definition.CheckpointFormat,
			&requiredNow,
			&optionalLater,
			&promotedRoots,
			&promotionGates,
			&definition.SystemGap,
			&catalogVisible,
			&definition.StatusMethodKey,
			&modelJSON,
		); err != nil {
			return nil, err
		}
		if scheduler.Valid {
			definition.Scheduler = stringPtr(scheduler.String)
		}
		definition.CatalogVisible = intToBool(catalogVisible)
		if err := json.Unmarshal([]byte(requiredNow), &definition.RequiredInputsNow); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(optionalLater), &definition.OptionalInputsLater); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(promotedRoots), &definition.PromotedAssetRoots); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(promotionGates), &definition.LivePromotionGates); err != nil {
			return nil, err
		}
		if strings.TrimSpace(modelJSON) != "" && modelJSON != "null" {
			var model modelOption
			if err := json.Unmarshal([]byte(modelJSON), &model); err != nil {
				return nil, err
			}
			definition.Model = &model
		}
		definitions = append(definitions, definition)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	jobsByContract, err := s.jobsByContract()
	if err != nil {
		return nil, err
	}
	for i := range definitions {
		definitions[i].Jobs = jobsByContract[definitions[i].ContractKey]
	}
	return definitions, nil
}

func (s *registryStore) jobsByContract() (map[string][]jobDefinition, error) {
	rows, err := s.db.Query(`SELECT contract_key, job_type, runner, required_inputs_json, requests_gpu FROM jobs ORDER BY contract_key, job_type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]jobDefinition{}
	for rows.Next() {
		var contractKey string
		var job jobDefinition
		var requiredInputs string
		var requestsGPU int
		if err := rows.Scan(&contractKey, &job.JobType, &job.Runner, &requiredInputs, &requestsGPU); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(requiredInputs), &job.RequiredInputs); err != nil {
			return nil, err
		}
		job.RequestsGPU = intToBool(requestsGPU)
		result[contractKey] = append(result[contractKey], job)
	}
	return result, rows.Err()
}

func (s *registryStore) ContractByKey(contractKey string) (contractDefinition, bool) {
	definitions, err := s.Contracts()
	if err != nil {
		return contractDefinition{}, false
	}
	for _, definition := range definitions {
		if definition.ContractKey == contractKey {
			return definition, true
		}
	}
	return contractDefinition{}, false
}

func (s *registryStore) LiveJobDefinition(jobType string) (jobDefinition, contractDefinition, bool) {
	definitions, err := s.Contracts()
	if err != nil {
		return jobDefinition{}, contractDefinition{}, false
	}
	for _, definition := range definitions {
		if definition.ContractStatus != "live" {
			continue
		}
		for _, job := range definition.Jobs {
			if job.JobType == jobType {
				return job, definition, true
			}
		}
	}
	return jobDefinition{}, contractDefinition{}, false
}

func (s *registryStore) CatalogContractDefinitions() []contractDefinition {
	definitions, err := s.Contracts()
	if err != nil {
		return nil
	}
	result := make([]contractDefinition, 0)
	for _, definition := range definitions {
		if definition.CatalogVisible {
			result = append(result, definition)
		}
	}
	return result
}

func (s *registryStore) LiveModelOptions() []modelOption {
	definitions, err := s.Contracts()
	if err != nil {
		return nil
	}
	options := make([]modelOption, 0)
	for _, definition := range definitions {
		if definition.Model == nil {
			continue
		}
		options = append(options, projectModelOption(definition))
	}
	return options
}

func (s *registryStore) ContractForSummaryPayload(payload map[string]any) (contractDefinition, bool) {
	method, _ := payload["method"].(string)
	if method == "" {
		return contractDefinition{}, false
	}
	definitions, err := s.Contracts()
	if err != nil {
		return contractDefinition{}, false
	}
	runtime, ok := payload["runtime"].(map[string]any)
	if ok {
		backend, _ := runtime["backend"].(string)
		scheduler, _ := runtime["scheduler"].(string)
		for _, definition := range definitions {
			if definition.AttackFamily != method || definition.Backend != backend {
				continue
			}
			if definition.Scheduler != nil {
				if scheduler == *definition.Scheduler {
					return definition, true
				}
				continue
			}
			if scheduler == "" {
				return definition, true
			}
		}
	}
	candidates := make([]contractDefinition, 0)
	for _, definition := range definitions {
		if definition.AttackFamily == method {
			candidates = append(candidates, definition)
		}
	}
	if len(candidates) == 0 {
		return contractDefinition{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return summaryFallbackPriority(candidates[i], method) > summaryFallbackPriority(candidates[j], method)
	})
	return candidates[0], true
}

func summaryFallbackPriority(definition contractDefinition, method string) int {
	score := 0
	if definition.StatusMethodKey == method {
		score += 8
	}
	if definition.ContractStatus == "live" {
		score += 4
	}
	if definition.CatalogVisible {
		score += 2
	}
	if definition.Availability == "ready" {
		score++
	}
	return score
}

func mustDefaultRegistryStore() *registryStore {
	store, err := defaultRegistryStore()
	if err != nil {
		panic(fmt.Sprintf("default registry store init failed: %v", err))
	}
	return store
}

func openRegistryStoreOrDefault(path string) *registryStore {
	if strings.TrimSpace(path) != "" {
		if store, err := openRegistryStore(path); err == nil {
			return store
		}
	}
	return mustDefaultRegistryStore()
}
