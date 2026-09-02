package registrysetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistrySetupCodecIsStrictAndCredentialFree(t *testing.T) {
	contents, err := Encode(validSetup())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"password", "username", ".dockerconfigjson", "token"} {
		if strings.Contains(strings.ToLower(string(contents)), forbidden) {
			t.Fatalf("encoded setup contains %q: %s", forbidden, contents)
		}
	}
	path := filepath.Join(t.TempDir(), "registry.yaml")
	if err = os.WriteFile(path, append(contents, []byte("unknown: true\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(path); err == nil || !strings.Contains(err.Error(), "field unknown not found") {
		t.Fatalf("strict decode error=%v", err)
	}
}
