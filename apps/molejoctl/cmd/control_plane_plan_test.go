package cmd

import (
	"strings"
	"testing"
)

func TestBuildControlPlaneInstallPlan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		observed controlPlaneObservedState
		want     controlPlaneInstallPlan
		wantErr  string
	}{
		{
			name: "fresh cluster",
			want: controlPlaneInstallPlan{
				createDatabaseCredentials: true, createBootstrapIdentity: true,
				createAgentCA: true, createServerIdentity: true, configureAgent: true, installChart: true,
			},
		},
		{
			name: "installed and paired",
			observed: controlPlaneObservedState{
				databaseCredentials: true, bootstrapIdentity: true, agentCA: true,
				serverIdentity: true, databasePVC: true, releaseInstalled: true, agentPaired: true,
			},
			want: controlPlaneInstallPlan{},
		},
		{
			name:     "PVC without credentials",
			observed: controlPlaneObservedState{databasePVC: true},
			wantErr:  "PVC exists without",
		},
		{
			name: "release missing required secret",
			observed: controlPlaneObservedState{
				databaseCredentials: true, bootstrapIdentity: true, agentCA: true, releaseInstalled: true,
			},
			wantErr: "release exists without",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := buildControlPlaneInstallPlan(test.observed)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error=%v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("plan=%+v err=%v, want %+v", got, err, test.want)
			}
		})
	}
}
