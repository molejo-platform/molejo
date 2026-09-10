package secretdelivery

import "testing"

func TestOnlyMaterializedKubernetesSecretIsImplemented(t *testing.T) {
	if err := Validate(MaterializedKubernetesSecret); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []Mode{"", "CSI", "ExternalSecrets"} {
		if err := Validate(mode); err == nil {
			t.Fatalf("unsupported mode %q was accepted", mode)
		}
	}
}
