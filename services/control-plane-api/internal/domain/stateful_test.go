package domain

import "testing"

func TestValidateWorkloadConfiguration(t *testing.T) {
	tests := []struct {
		name          string
		kind          WorkloadKind
		configuration RuntimeConfig
		volume        *VolumeRequest
		wantErr       bool
	}{
		{
			name:          "stateless does not require storage",
			kind:          WorkloadStateless,
			configuration: validIntentConfiguration(),
		},
		{
			name:          "stateless rejects a volume",
			kind:          WorkloadStateless,
			configuration: validIntentConfiguration(),
			volume:        &VolumeRequest{StorageProfileID: "persistent-standard", SizeGiB: 1, MountPath: "/data"},
			wantErr:       true,
		},
		{
			name:          "stateful requires a volume",
			kind:          WorkloadStateful,
			configuration: validIntentConfiguration(),
			wantErr:       true,
		},
		{
			name:          "stateful accepts one replica and one volume",
			kind:          WorkloadStateful,
			configuration: validIntentConfiguration(),
			volume:        &VolumeRequest{StorageProfileID: "persistent-standard", SizeGiB: 1, MountPath: "/data"},
		},
		{
			name: "stateful rejects multiple replicas",
			kind: WorkloadStateful,
			configuration: func() RuntimeConfig {
				configuration := validIntentConfiguration()
				configuration.Replicas = 2
				return configuration
			}(),
			volume:  &VolumeRequest{StorageProfileID: "persistent-standard", SizeGiB: 1, MountPath: "/data"},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateWorkloadConfiguration(test.kind, test.configuration, test.volume)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateWorkloadConfiguration() error=%v wantErr=%v", err, test.wantErr)
			}
		})
	}
}

func TestValidateVolumeExpansion(t *testing.T) {
	tests := []struct {
		name              string
		currentSizeGiB    int64
		requestedSizeGiB  int64
		profileMaximumGiB int64
		wantErr           bool
	}{
		{name: "expands upward", currentSizeGiB: 1, requestedSizeGiB: 2, profileMaximumGiB: 10},
		{name: "rejects same size", currentSizeGiB: 2, requestedSizeGiB: 2, profileMaximumGiB: 10, wantErr: true},
		{name: "rejects shrink", currentSizeGiB: 2, requestedSizeGiB: 1, profileMaximumGiB: 10, wantErr: true},
		{name: "rejects profile overflow", currentSizeGiB: 2, requestedSizeGiB: 11, profileMaximumGiB: 10, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateVolumeExpansion(test.currentSizeGiB, test.requestedSizeGiB, test.profileMaximumGiB)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateVolumeExpansion() error=%v wantErr=%v", err, test.wantErr)
			}
		})
	}
}

func TestAppVolumeCanOnlyBeRemovedAfterRuntimeDetach(t *testing.T) {
	volume := AppVolume{DesiredState: VolumeDesiredDeleted, Attached: true}
	if err := ValidateVolumeRemoval(volume); err == nil {
		t.Fatal("ValidateVolumeRemoval accepted an attached volume")
	}
	volume.Attached = false
	if err := ValidateVolumeRemoval(volume); err != nil {
		t.Fatalf("ValidateVolumeRemoval rejected a detached volume: %v", err)
	}
}

func validIntentConfiguration() RuntimeConfig {
	return ConfigurationFromIntent(validIntent())
}
