package controlplaneinstall

import (
	"strings"
	"testing"
)

func TestNormalizePublicOptions(t *testing.T) {
	options, err := normalizeAndValidateOptions(Options{
		PublicHost:       "Cloud.Molejo.Dev.",
		GatewayNamespace: "molejo-system",
		GatewayName:      "molejo",
		GatewaySection:   "https-molejo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.PublicHost != "cloud.molejo.dev" {
		t.Fatalf("public host=%q", options.PublicHost)
	}
	values := controlPlaneChartValues(options, "local-path")
	public := values["public"].(map[string]any)
	if public["enabled"] != true || public["host"] != "cloud.molejo.dev" {
		t.Fatalf("public values=%+v", public)
	}
	if !publicConfigurationMatches(values, options) {
		t.Fatalf("public configuration did not match values=%+v options=%+v", values, options)
	}
}

func TestNormalizePublicOptionsRejectsURL(t *testing.T) {
	_, err := normalizeAndValidateOptions(Options{
		PublicHost:       "https://cloud.molejo.dev",
		GatewayNamespace: "molejo-system",
		GatewayName:      "molejo",
		GatewaySection:   "https-molejo",
	})
	if err == nil || !strings.Contains(err.Error(), "DNS name") {
		t.Fatalf("error=%v", err)
	}
}

func TestPublicConfigurationDoesNotMatchDifferentGateway(t *testing.T) {
	options := Options{PublicHost: "cloud.molejo.dev", GatewayNamespace: "molejo-system", GatewayName: "molejo", GatewaySection: "https-molejo"}
	values := controlPlaneChartValues(options, "")
	options.GatewayName = "other"
	if publicConfigurationMatches(values, options) {
		t.Fatal("different Gateway matched installed values")
	}
}
