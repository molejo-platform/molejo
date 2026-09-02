package registrysetup

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

func Load(path string) (Setup, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Setup{}, fmt.Errorf("read registry setup: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	var setup Setup
	if err = decoder.Decode(&setup); err != nil {
		return Setup{}, fmt.Errorf("decode registry setup: %w", err)
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Setup{}, errors.New("decode registry setup: multiple YAML documents are not supported")
		}
		return Setup{}, fmt.Errorf("decode registry setup: %w", err)
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
		return nil, fmt.Errorf("encode registry setup: %w", err)
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
	return fmt.Errorf("invalid registry setup: %s", message)
}
