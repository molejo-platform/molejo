package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/molejo-platform/molejo/packages/workspacecontract"
)

type config struct {
	Namespace                 string
	IdentitySecret            string
	EnrollmentSecret          string
	EnrollmentURL             string
	EnrollmentCAFile          string
	GRPCAddress               string
	GRPCServerName            string
	HealthAddress             string
	WorkspaceProvisioningMode workspacecontract.ProvisioningMode
}

func loadConfig() (config, error) {
	configuration := config{
		Namespace:        strings.TrimSpace(os.Getenv("POD_NAMESPACE")),
		IdentitySecret:   env("MOLEJO_AGENT_IDENTITY_SECRET", "molejo-agent-identity"),
		EnrollmentSecret: env("MOLEJO_AGENT_ENROLLMENT_SECRET", "molejo-agent-enrollment"),
		EnrollmentURL:    strings.TrimSpace(os.Getenv("MOLEJO_AGENT_ENROLLMENT_URL")),
		EnrollmentCAFile: strings.TrimSpace(os.Getenv("MOLEJO_AGENT_ENROLLMENT_CA_FILE")),
		GRPCAddress:      strings.TrimSpace(os.Getenv("MOLEJO_AGENT_GRPC_ADDRESS")),
		GRPCServerName:   strings.TrimSpace(os.Getenv("MOLEJO_AGENT_GRPC_SERVER_NAME")),
		HealthAddress:    env("MOLEJO_AGENT_HEALTH_ADDR", ":8081"),
	}
	if configuration.Namespace == "" {
		return config{}, fmt.Errorf("POD_NAMESPACE is required")
	}
	if (configuration.GRPCAddress == "") != (configuration.GRPCServerName == "") {
		return config{}, fmt.Errorf("Agent gRPC configuration is incomplete")
	}
	mode, ok := workspacecontract.ParseProvisioningMode(os.Getenv("MOLEJO_WORKSPACE_PROVISIONING_MODE"))
	if !ok {
		return config{}, fmt.Errorf("MOLEJO_WORKSPACE_PROVISIONING_MODE must be Disabled or Namespaced")
	}
	configuration.WorkspaceProvisioningMode = mode
	return configuration, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
