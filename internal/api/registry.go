package api

type modelOption struct {
	Key            string  `json:"key"`
	Label          string  `json:"label"`
	AccessLevel    string  `json:"access_level"`
	Availability   string  `json:"availability"`
	Paper          string  `json:"paper"`
	Method         string  `json:"method"`
	Backend        string  `json:"backend"`
	Scheduler      *string `json:"scheduler"`
	ContractKey    string  `json:"contract_key,omitempty"`
	Track          string  `json:"track,omitempty"`
	AttackFamily   string  `json:"attack_family,omitempty"`
	TargetKey      string  `json:"target_key,omitempty"`
	ContractStatus string  `json:"contract_status,omitempty"`
	CatalogVisible bool    `json:"catalog_visible,omitempty"`
}

type jobDefinition struct {
	JobType        string   `json:"job_type"`
	Runner         string   `json:"runner"`
	RequiredInputs []string `json:"required_inputs"`
	RequestsGPU    bool     `json:"requests_gpu"`
}

type contractDefinition struct {
	ContractKey          string          `json:"contract_key"`
	Track                string          `json:"track"`
	AttackFamily         string          `json:"attack_family"`
	TargetKey            string          `json:"target_key"`
	Label                string          `json:"label"`
	Paper                string          `json:"paper"`
	Backend              string          `json:"backend"`
	Scheduler            *string         `json:"scheduler"`
	Availability         string          `json:"availability"`
	DefaultEvidenceLevel string          `json:"default_evidence_level"`
	ContractStatus       string          `json:"contract_status"`
	RegistryEvidence     string          `json:"registry_evidence"`
	FeatureAccess        string          `json:"feature_access"`
	CheckpointFormat     string          `json:"checkpoint_format"`
	RequiredInputsNow    []string        `json:"required_inputs_now"`
	OptionalInputsLater  []string        `json:"optional_inputs_later"`
	PromotedAssetRoots   []string        `json:"promoted_asset_roots"`
	LivePromotionGates   []string        `json:"live_promotion_gates"`
	SystemGap            string          `json:"system_gap"`
	CatalogVisible       bool            `json:"catalog_visible"`
	Model                *modelOption    `json:"model"`
	Jobs                 []jobDefinition `json:"jobs"`
	StatusMethodKey      string          `json:"status_method_key"`
}

type contractProjection struct {
	ContractKey    string
	Track          string
	AttackFamily   string
	TargetKey      string
	Availability   string
	EvidenceLevel  string
	Label          string
	Paper          string
	Backend        string
	Scheduler      *string
	ContractStatus string
	CatalogVisible bool
}

var contractRegistry = []contractDefinition{
	{
		ContractKey:          "black-box/recon/sd15-ddim",
		Track:                "black-box",
		AttackFamily:         "recon",
		TargetKey:            "sd15-ddim",
		Label:                "Stable Diffusion 1.5 + DDIM",
		Paper:                "BlackBox_Reconstruction_ArXiv2023",
		Backend:              "stable_diffusion",
		Scheduler:            stringPtr("ddim"),
		Availability:         "ready",
		DefaultEvidenceLevel: "catalog",
		ContractStatus:       "live",
		RegistryEvidence:     "runtime-ready",
		FeatureAccess:        "none",
		CheckpointFormat:     "directory-state",
		RequiredInputsNow:    []string{"artifact_dir"},
		OptionalInputsLater: []string{
			"target_model_dir",
			"shadow_model_dir",
			"runtime_dataset_payload",
		},
		LivePromotionGates: []string{
			"runtime and artifact intake remain admitted in public api",
			"blackbox-status best evidence path resolves to a matching recon summary",
		},
		SystemGap:       "public Local-API contract still models artifact replay more fully than runtime intake",
		CatalogVisible:  true,
		StatusMethodKey: "recon",
		Model: &modelOption{
			Key:          "sd15-ddim",
			Label:        "Stable Diffusion 1.5 + DDIM",
			AccessLevel:  "black-box",
			Availability: "ready",
			Paper:        "BlackBox_Reconstruction_ArXiv2023",
			Method:       "recon",
			Backend:      "stable_diffusion",
			Scheduler:    stringPtr("ddim"),
		},
		Jobs: []jobDefinition{
			{
				JobType:        "recon_artifact_mainline",
				Runner:         "recon_artifact_mainline",
				RequiredInputs: []string{"artifact_dir"},
				RequestsGPU:    false,
			},
			{
				JobType:        "recon_runtime_mainline",
				Runner:         "recon_runtime_mainline",
				RequiredInputs: nil,
				RequestsGPU:    true,
			},
		},
	},
	{
		ContractKey:          "black-box/recon/kandinsky-v22",
		Track:                "black-box",
		AttackFamily:         "recon",
		TargetKey:            "kandinsky-v22",
		Label:                "Kandinsky v2.2",
		Paper:                "BlackBox_Reconstruction_ArXiv2023",
		Backend:              "kandinsky_v22",
		Availability:         "partial",
		DefaultEvidenceLevel: "catalog",
		ContractStatus:       "live",
		RegistryEvidence:     "artifact-summary",
		FeatureAccess:        "none",
		CheckpointFormat:     "directory-state",
		SystemGap:            "no public runnable job contract is admitted yet",
		CatalogVisible:       true,
		Model: &modelOption{
			Key:          "kandinsky-v22",
			Label:        "Kandinsky v2.2",
			AccessLevel:  "black-box",
			Availability: "partial",
			Paper:        "BlackBox_Reconstruction_ArXiv2023",
			Method:       "recon",
			Backend:      "kandinsky_v22",
		},
	},
	{
		ContractKey:          "black-box/sample/dit-xl2-256",
		Track:                "black-box",
		AttackFamily:         "sample",
		TargetKey:            "dit-xl2-256",
		Label:                "DiT-XL/2 256",
		Paper:                "Scalable_Diffusion_Transformers_2022",
		Backend:              "dit",
		Availability:         "partial",
		DefaultEvidenceLevel: "catalog",
		ContractStatus:       "target",
		RegistryEvidence:     "sample-smoke",
		FeatureAccess:        "none",
		CheckpointFormat:     "directory-state",
		SystemGap:            "not part of the current live catalog or job contract",
		Model: &modelOption{
			Key:          "dit-xl2-256",
			Label:        "DiT-XL/2 256",
			AccessLevel:  "black-box",
			Availability: "partial",
			Paper:        "Scalable_Diffusion_Transformers_2022",
			Method:       "sample",
			Backend:      "dit",
		},
	},
	{
		ContractKey:          "gray-box/pia/cifar10-ddpm",
		Track:                "gray-box",
		AttackFamily:         "pia",
		TargetKey:            "cifar10-ddpm",
		Label:                "PIA on CIFAR10 DDPM",
		Paper:                "PIA",
		Backend:              "",
		Availability:         "ready",
		DefaultEvidenceLevel: "catalog",
		ContractStatus:       "live",
		RegistryEvidence:     "runtime-mainline",
		FeatureAccess:        "epsilon_t",
		CheckpointFormat:     "single-file",
		RequiredInputsNow: []string{
			"config",
		},
		OptionalInputsLater: []string{
			"member_split_root",
			"device",
			"num_samples",
			"batch_size",
			"stochastic_dropout_defense",
		},
		PromotedAssetRoots: []string{
			"Project/workspaces/gray-box/assets/pia/checkpoints",
			"Project/workspaces/gray-box/assets/pia/datasets",
		},
		LivePromotionGates: []string{
			"line-owned promoted checkpoint and dataset roots exist",
			"stable admitted job_type and runner are implemented",
			"summary hydration rule is proven against real non-smoke evidence",
			"asset grade and provenance status are approved for live catalog exposure",
		},
		SystemGap:       "result hydration and platform contract still need to ingest gray-box live summaries",
		CatalogVisible:  true,
		StatusMethodKey: "pia",
		Model: &modelOption{
			Key:          "pia-cifar10-ddpm",
			Label:        "PIA on CIFAR10 DDPM",
			AccessLevel:  "gray-box",
			Availability: "ready",
			Paper:        "PIA",
			Method:       "pia",
			Backend:      "",
		},
		Jobs: []jobDefinition{
			{
				JobType:        "pia_runtime_mainline",
				Runner:         "pia_runtime_mainline",
				RequiredInputs: []string{"config"},
				RequestsGPU:    false,
			},
		},
	},
	{
		ContractKey:          "white-box/gsa/ddpm-cifar10",
		Track:                "white-box",
		AttackFamily:         "gsa",
		TargetKey:            "ddpm-cifar10",
		Label:                "GSA on DDPM CIFAR10",
		Paper:                "GSA",
		Backend:              "",
		Availability:         "partial",
		DefaultEvidenceLevel: "catalog",
		ContractStatus:       "live",
		RegistryEvidence:     "runtime-mainline",
		FeatureAccess:        "gradient",
		CheckpointFormat:     "directory-state",
		RequiredInputsNow: []string{
			"assets_root",
		},
		OptionalInputsLater: []string{
			"resolution",
			"ddpm_num_steps",
			"sampling_frequency",
			"attack_method",
			"prediction_type",
		},
		PromotedAssetRoots: []string{
			"Project/workspaces/white-box/assets/gsa/checkpoints",
			"Project/workspaces/white-box/assets/gsa/datasets",
		},
		LivePromotionGates: []string{
			"promoted gsa asset roots exist with runnable checkpoint layout",
			"white-box diff-audit adapter and admitted job_type are implemented",
			"non-smoke member/non-member inputs are available",
			"summary hydration rule is proven against non-toy evidence",
		},
		SystemGap:       "white-box runtime is live but current metrics are still tiny-sample local evidence",
		CatalogVisible:  true,
		StatusMethodKey: "gsa",
		Model: &modelOption{
			Key:          "gsa-ddpm-cifar10",
			Label:        "GSA on DDPM CIFAR10",
			AccessLevel:  "white-box",
			Availability: "partial",
			Paper:        "GSA",
			Method:       "gsa",
			Backend:      "",
		},
		Jobs: []jobDefinition{
			{
				JobType:        "gsa_runtime_mainline",
				Runner:         "gsa_runtime_mainline",
				RequiredInputs: []string{"assets_root"},
				RequestsGPU:    false,
			},
		},
	},
}

func liveModelOptions() []modelOption {
	return mustDefaultRegistryStore().LiveModelOptions()
}

func catalogContractDefinitions() []contractDefinition {
	return mustDefaultRegistryStore().CatalogContractDefinitions()
}

func liveJobDefinition(jobType string) (jobDefinition, contractDefinition, bool) {
	return mustDefaultRegistryStore().LiveJobDefinition(jobType)
}

func contractDefinitionByKey(contractKey string) (contractDefinition, bool) {
	return mustDefaultRegistryStore().ContractByKey(contractKey)
}

func projectContract(definition contractDefinition) contractProjection {
	return contractProjection{
		ContractKey:    definition.ContractKey,
		Track:          definition.Track,
		AttackFamily:   definition.AttackFamily,
		TargetKey:      definition.TargetKey,
		Availability:   definition.Availability,
		EvidenceLevel:  definition.DefaultEvidenceLevel,
		Label:          definition.Label,
		Paper:          definition.Paper,
		Backend:        definition.Backend,
		Scheduler:      definition.Scheduler,
		ContractStatus: definition.ContractStatus,
		CatalogVisible: definition.CatalogVisible,
	}
}

func projectModelOption(definition contractDefinition) modelOption {
	model := *definition.Model
	projection := projectContract(definition)
	model.ContractKey = projection.ContractKey
	model.Track = projection.Track
	model.AttackFamily = projection.AttackFamily
	model.TargetKey = projection.TargetKey
	model.ContractStatus = projection.ContractStatus
	model.CatalogVisible = projection.CatalogVisible
	return model
}

func projectCatalogEntry(definition contractDefinition) catalogEntry {
	projection := projectContract(definition)
	return catalogEntry{
		ContractKey:     projection.ContractKey,
		Track:           projection.Track,
		AttackFamily:    projection.AttackFamily,
		TargetKey:       projection.TargetKey,
		Availability:    projection.Availability,
		EvidenceLevel:   projection.EvidenceLevel,
		Label:           projection.Label,
		Paper:           projection.Paper,
		Backend:         projection.Backend,
		Scheduler:       projection.Scheduler,
		BestSummaryPath: nil,
		BestWorkspace:   nil,
	}
}

func contractForSummaryPayload(payload map[string]any) (contractDefinition, bool) {
	return mustDefaultRegistryStore().ContractForSummaryPayload(payload)
}
