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
	JobType        string
	Runner         string
	RequiredInputs []string
	RequestsGPU    bool
}

type contractDefinition struct {
	ContractKey          string
	Track                string
	AttackFamily         string
	TargetKey            string
	Label                string
	Paper                string
	Backend              string
	Scheduler            *string
	Availability         string
	DefaultEvidenceLevel string
	ContractStatus       string
	RegistryEvidence     string
	FeatureAccess        string
	CheckpointFormat     string
	RequiredInputsNow    []string
	OptionalInputsLater  []string
	PromotedAssetRoots   []string
	LivePromotionGates   []string
	SystemGap            string
	CatalogVisible       bool
	Model                *modelOption
	Jobs                 []jobDefinition
	StatusMethodKey      string
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
		Availability:         "planned",
		DefaultEvidenceLevel: "catalog",
		ContractStatus:       "target",
		RegistryEvidence:     "real-asset-probe-ready",
		FeatureAccess:        "epsilon_t",
		CheckpointFormat:     "single-file",
		RequiredInputsNow: []string{
			"dataset_name",
			"dataset_root",
			"model_dir",
			"member_split_root",
			"observable_contract_level",
		},
		OptionalInputsLater: []string{
			"preview_batch_size",
			"gpu_runtime_profile",
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
		SystemGap: "missing unified runtime mainline command and admitted Local-API live job contract",
	},
	{
		ContractKey:          "white-box/gsa/ddpm-cifar10",
		Track:                "white-box",
		AttackFamily:         "gsa",
		TargetKey:            "ddpm-cifar10",
		Label:                "GSA on DDPM CIFAR10",
		Paper:                "GSA",
		Availability:         "planned",
		DefaultEvidenceLevel: "catalog",
		ContractStatus:       "target",
		RegistryEvidence:     "gradient-smoke",
		FeatureAccess:        "gradient",
		CheckpointFormat:     "directory-state",
		RequiredInputsNow: []string{
			"train_data_dir",
			"gradient_extraction_spec",
		},
		OptionalInputsLater: []string{
			"target_checkpoint_path",
			"shadow_checkpoint_paths",
			"member_split_root",
			"activation_hook_spec",
		},
		PromotedAssetRoots: []string{
			"Project/workspaces/white-box/assets/gsa/models",
			"Project/workspaces/white-box/assets/gsa/datasets",
		},
		LivePromotionGates: []string{
			"promoted gsa asset roots exist with runnable checkpoint layout",
			"white-box diff-audit adapter and admitted job_type are implemented",
			"non-smoke member/non-member inputs are available",
			"summary hydration rule is proven against non-toy evidence",
		},
		SystemGap: "no diff-audit adapter, no admitted Local-API live job contract, and no promoted live asset root yet",
	},
}

func liveModelOptions() []modelOption {
	options := make([]modelOption, 0)
	for _, definition := range contractRegistry {
		if definition.Model == nil {
			continue
		}
		model := *definition.Model
		model.ContractKey = definition.ContractKey
		model.Track = definition.Track
		model.AttackFamily = definition.AttackFamily
		model.TargetKey = definition.TargetKey
		model.ContractStatus = definition.ContractStatus
		model.CatalogVisible = definition.CatalogVisible
		options = append(options, model)
	}
	return options
}

func catalogContractDefinitions() []contractDefinition {
	definitions := make([]contractDefinition, 0)
	for _, definition := range contractRegistry {
		if definition.CatalogVisible {
			definitions = append(definitions, definition)
		}
	}
	return definitions
}

func liveJobDefinition(jobType string) (jobDefinition, contractDefinition, bool) {
	for _, definition := range contractRegistry {
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

func contractDefinitionByKey(contractKey string) (contractDefinition, bool) {
	for _, definition := range contractRegistry {
		if definition.ContractKey == contractKey {
			return definition, true
		}
	}
	return contractDefinition{}, false
}

func contractForSummaryPayload(payload map[string]any) (contractDefinition, bool) {
	method, _ := payload["method"].(string)
	if method == "" {
		return contractDefinition{}, false
	}

	runtime, ok := payload["runtime"].(map[string]any)
	if ok {
		backend, _ := runtime["backend"].(string)
		scheduler, _ := runtime["scheduler"].(string)
		for _, definition := range contractRegistry {
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

	for _, definition := range contractRegistry {
		if definition.AttackFamily == method {
			return definition, true
		}
	}
	return contractDefinition{}, false
}
