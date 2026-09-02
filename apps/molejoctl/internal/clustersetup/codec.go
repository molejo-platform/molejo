package clustersetup

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func Load(path string) (Setup, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Setup{}, fmt.Errorf("read cluster setup: %w", err)
	}
	var setup Setup
	if err = yaml.Unmarshal(contents, &setup); err != nil {
		return Setup{}, fmt.Errorf("decode cluster setup: %w", err)
	}
	return setup, nil
}

func Encode(setup Setup) ([]byte, error) {
	normalized, diagnostics := NormalizeAndValidate(setup)
	if len(diagnostics) > 0 {
		return nil, diagnosticsError(diagnostics)
	}
	contents, err := yaml.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode cluster setup: %w", err)
	}
	return contents, nil
}

func diagnosticsError(diagnostics []Diagnostic) error {
	message := ""
	for index, diagnostic := range diagnostics {
		if index > 0 {
			message += "; "
		}
		message += diagnostic.Field + ": " + diagnostic.Message
	}
	return fmt.Errorf("invalid cluster setup: %s", message)
}
