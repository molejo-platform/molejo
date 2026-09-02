package clustertls

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func LoadSetup(path string) (Setup, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Setup{}, fmt.Errorf("read TLS setup: %w", err)
	}
	var setup Setup
	if err = yaml.Unmarshal(contents, &setup); err != nil {
		return Setup{}, fmt.Errorf("decode TLS setup: %w", err)
	}
	return setup, nil
}
