package conformance

import (
	"context"
	"fmt"
)

type cluster struct {
	ID                string   `json:"id"`
	Status            string   `json:"status"`
	ClusterUID        string   `json:"clusterUid"`
	KubernetesVersion string   `json:"kubernetesVersion"`
	AgentVersion      string   `json:"agentVersion"`
	Capabilities      []string `json:"capabilities"`
}

func verifyTarget(ctx context.Context, client *Client, target Target) (TargetEvidence, error) {
	var observed cluster
	if err := client.Get(ctx, "/api/v1/admin/clusters/"+target.ClusterID, &observed); err != nil {
		return TargetEvidence{}, fmt.Errorf("read target cluster: %w", err)
	}
	if observed.ID != target.ClusterID {
		return TargetEvidence{}, fmt.Errorf("target cluster response id=%q, want %q", observed.ID, target.ClusterID)
	}
	if observed.Status != "Active" {
		return TargetEvidence{}, fmt.Errorf("target cluster status=%q, want Active", observed.Status)
	}
	if target.ExpectedClusterUID != "" && observed.ClusterUID != target.ExpectedClusterUID {
		return TargetEvidence{}, fmt.Errorf("target cluster UID=%q, want %q", observed.ClusterUID, target.ExpectedClusterUID)
	}
	return TargetEvidence{
		Endpoint: target.Endpoint, ClusterID: observed.ID, ClusterUID: observed.ClusterUID,
		KubeContext: target.KubeContext, KubernetesVersion: observed.KubernetesVersion,
		AgentVersion: observed.AgentVersion, Capabilities: observed.Capabilities, Disposable: target.Disposable,
	}, nil
}

func VerifyTarget(ctx context.Context, client *Client, target Target) (TargetEvidence, error) {
	return verifyTarget(ctx, client, target)
}
