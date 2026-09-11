package publication

import (
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

func Load(path string) (Setup, error) {
	file, err := os.Open(path)
	if err != nil {
		return Setup{}, fmt.Errorf("read publication setup: %w", err)
	}
	defer file.Close()
	decoder := yaml.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.KnownFields(true)
	var setup Setup
	if err = decoder.Decode(&setup); err != nil {
		return Setup{}, fmt.Errorf("decode publication setup: %w", err)
	}
	var extra any
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple YAML documents are not supported")
		}
		return Setup{}, fmt.Errorf("decode publication setup: %w", err)
	}
	return setup, nil
}
