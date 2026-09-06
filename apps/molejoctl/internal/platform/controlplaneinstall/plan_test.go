package controlplaneinstall

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
				createAgentCA: true, createServerCA: true, createServerIdentity: true, configureAgent: true, installChart: true,
			},
		},
		{
			name: "installed and paired",
			observed: controlPlaneObservedState{
				databaseCredentials: true, bootstrapIdentity: true, agentCA: true,
				serverCA: true, serverIdentity: true, databasePVC: true, releaseInstalled: true, agentPaired: true,
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
		{
			name: "current release missing server trust root",
			observed: controlPlaneObservedState{
				databaseCredentials: true, bootstrapIdentity: true, agentCA: true, serverIdentity: true,
				releaseInstalled: true, serverCATrustUsed: true,
			},
			wantErr: "restore its Secret",
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

func TestDedicatedServerCATrustMigration(t *testing.T) {
	for _, test := range []struct {
		name      string
		installed bool
		observed  controlPlaneObservedState
		want      bool
	}{
		{name: "fresh installation", want: true},
		{name: "legacy installed chart", installed: true, want: false},
		{name: "current chart", installed: true, observed: controlPlaneObservedState{serverCA: true, serverCATrustUsed: true}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := useDedicatedServerCA(test.installed, test.observed)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("use dedicated server CA=%v, want %v", got, test.want)
			}
		})
	}
	for _, observed := range []controlPlaneObservedState{
		{serverCA: true},
		{serverCATrustUsed: true},
	} {
		if _, err := useDedicatedServerCA(true, observed); err == nil {
			t.Fatalf("partial dedicated server CA state %+v was accepted", observed)
		}
	}
}
