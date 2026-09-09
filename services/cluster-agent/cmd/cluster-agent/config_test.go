package main

import "testing"

func TestLoadConfigRequiresNamespaceAndCompleteGRPCTarget(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "")
	if _, err := loadConfig(); err == nil {
		t.Fatal("missing namespace was accepted")
	}

	t.Setenv("POD_NAMESPACE", "molejo-system")
	t.Setenv("MOLEJO_AGENT_GRPC_ADDRESS", "control-plane:8443")
	t.Setenv("MOLEJO_AGENT_GRPC_SERVER_NAME", "")
	if _, err := loadConfig(); err == nil {
		t.Fatal("incomplete gRPC target was accepted")
	}

	t.Setenv("MOLEJO_AGENT_GRPC_SERVER_NAME", "control-plane")
	configuration, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.IdentitySecret != "molejo-agent-identity" || configuration.HealthAddress != ":8081" {
		t.Fatalf("defaults = %+v", configuration)
	}
	if configuration.WorkspaceProvisioningMode != "Disabled" {
		t.Fatalf("default provisioning mode = %q", configuration.WorkspaceProvisioningMode)
	}

	t.Setenv("MOLEJO_WORKSPACE_PROVISIONING_MODE", "Namespaced")
	configuration, err = loadConfig()
	if err != nil || configuration.WorkspaceProvisioningMode != "Namespaced" {
		t.Fatalf("namespaced provisioning configuration=%+v err=%v", configuration, err)
	}

	t.Setenv("MOLEJO_WORKSPACE_PROVISIONING_MODE", "enabled")
	if _, err = loadConfig(); err == nil {
		t.Fatal("unknown provisioning mode was accepted")
	}
}
