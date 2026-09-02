package registrysetup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
)

const maxDockerConfigBytes = 1 << 20

type dockerConfig struct {
	Auths map[string]json.RawMessage `json:"auths"`
}

type dockerCredential struct {
	Auth          string `json:"auth"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	IdentityToken string `json:"identitytoken"`
	RegistryToken string `json:"registrytoken"`
}

func ReadDockerConfigSource(path string, stdin io.Reader) ([]byte, error) {
	var reader io.Reader
	var closeFile func() error
	if path == "-" {
		if stdin == nil {
			return nil, errors.New("Docker config stdin is unavailable")
		}
		reader = stdin
	} else {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open Docker config: %w", err)
		}
		reader = file
		closeFile = file.Close
	}
	if closeFile != nil {
		defer closeFile()
	}
	contents, err := io.ReadAll(io.LimitReader(reader, maxDockerConfigBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Docker config: %w", err)
	}
	if len(contents) == 0 {
		return nil, errors.New("Docker config is empty")
	}
	if len(contents) > maxDockerConfigBytes {
		return nil, errors.New("Docker config exceeds 1 MiB")
	}
	return contents, nil
}

func FilterDockerConfig(contents []byte, host string) ([]byte, error) {
	var source dockerConfig
	decoder := json.NewDecoder(bytes.NewReader(contents))
	if err := decoder.Decode(&source); err != nil {
		return nil, errors.New("Docker config is not valid JSON")
	}
	var selected json.RawMessage
	for candidate, credential := range source.Auths {
		if normalizeRegistryKey(candidate) == host {
			selected = credential
			break
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("Docker config does not contain credentials for %s", host)
	}
	var credential dockerCredential
	if err := json.Unmarshal(selected, &credential); err != nil || !credential.usable() {
		return nil, fmt.Errorf("Docker config credentials for %s are empty or unsupported", host)
	}
	filtered, err := json.Marshal(dockerConfig{Auths: map[string]json.RawMessage{host: selected}})
	if err != nil {
		return nil, errors.New("encode filtered Docker config")
	}
	return filtered, nil
}

func DockerConfigContains(contents []byte, host string) bool {
	filtered, err := FilterDockerConfig(contents, host)
	return err == nil && len(filtered) > 0
}

func (credential dockerCredential) usable() bool {
	return credential.Auth != "" || credential.IdentityToken != "" || credential.RegistryToken != "" || credential.Username != "" && credential.Password != ""
}

func normalizeRegistryKey(value string) string {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		value = parsed.Host
	}
	value = strings.TrimPrefix(value, "//")
	value = strings.TrimSuffix(value, "/v1/")
	value = strings.TrimSuffix(value, "/v2/")
	return strings.TrimSuffix(value, "/")
}
