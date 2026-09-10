package domain

// WorkspaceSummary exposes application-loop facts without prescribing Console copy or task order.
type WorkspaceSummary struct {
	Counts     WorkspaceResourceCounts  `json:"counts"`
	Clusters   WorkspaceClusterCounts   `json:"clusters"`
	Runtime    WorkspaceRuntimeCounts   `json:"runtime"`
	Operations WorkspaceOperationCounts `json:"operations"`
}

type WorkspaceResourceCounts struct {
	Projects                int `json:"projects"`
	Environments            int `json:"environments"`
	Apps                    int `json:"apps"`
	AppEnvironments         int `json:"appEnvironments"`
	AppsWithSource          int `json:"appsWithSource"`
	AppsWithRelease         int `json:"appsWithRelease"`
	DeployedAppEnvironments int `json:"deployedAppEnvironments"`
}

type WorkspaceClusterCounts struct {
	Total int `json:"total"`
	Ready int `json:"ready"`
}

type WorkspaceRuntimeCounts struct {
	Ready       int `json:"ready"`
	Progressing int `json:"progressing"`
	Degraded    int `json:"degraded"`
}

type WorkspaceOperationCounts struct {
	Active int `json:"active"`
	Failed int `json:"failed"`
}
