package clustertls

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func LoadProfile(path string) (Profile, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, fmt.Errorf("read TLS profile: %w", err)
	}
	var profile Profile
	decoderError := yaml.Unmarshal(contents, &profile)
	if decoderError != nil {
		return Profile{}, fmt.Errorf("decode TLS profile: %w", decoderError)
	}
	return profile, nil
}

func EncodeBinding(binding Binding) ([]byte, error) {
	contents, err := yaml.Marshal(binding)
	if err != nil {
		return nil, fmt.Errorf("encode TLS binding: %w", err)
	}
	return contents, nil
}

func DecodeBinding(contents []byte) (Binding, error) {
	var binding Binding
	if err := yaml.Unmarshal(contents, &binding); err != nil {
		return Binding{}, fmt.Errorf("decode TLS binding: %w", err)
	}
	if binding.APIVersion != APIVersion || binding.Kind != BindingKind || binding.Metadata.Name == "" {
		return Binding{}, fmt.Errorf("TLS binding has an invalid identity")
	}
	return binding, nil
}
