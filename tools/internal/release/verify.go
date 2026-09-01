package release

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type githubRelease struct {
	TagName      string `json:"tagName"`
	URL          string `json:"url"`
	IsDraft      bool   `json:"isDraft"`
	IsPrerelease bool   `json:"isPrerelease"`
	Assets       []struct {
		Name string `json:"name"`
	} `json:"assets"`
}

func (p *Pipeline) Verify(ctx context.Context) error {
	if err := p.Check(ctx); err != nil {
		return err
	}
	directory := p.distDirectory()
	manifest, err := readManifest(filepath.Join(directory, "release-manifest.json"))
	if err != nil {
		return err
	}
	head, err := p.head(ctx)
	if err != nil {
		return err
	}
	if manifest.Version != p.Version || manifest.Tag != "v"+p.Version || manifest.Commit != head || manifest.Repository != GitHubRepository {
		return fmt.Errorf("release manifest does not describe version %s at commit %s", p.Version, head)
	}
	checksums, err := verifyChecksums(directory)
	if err != nil {
		return err
	}
	release, err := p.verifyGitHubRelease(ctx, checksums)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	for _, image := range Images {
		artifact := manifest.Images[image.Name]
		if artifact.Digest == "" {
			return fmt.Errorf("manifest is missing image %s", image.Name)
		}
		repository := "molejo-platform/" + image.Name
		if err = verifyAnonymousOCI(ctx, client, repository, "v"+p.Version, artifact.Digest); err != nil {
			return err
		}
	}
	if err = p.verifyCharts(ctx, directory, manifest, client); err != nil {
		return err
	}
	_, err = fmt.Fprintf(p.Out, "release verified: %s\n", release.URL)
	return err
}

func verifyChecksums(directory string) (map[string]string, error) {
	file, err := os.Open(filepath.Join(directory, "checksums.txt"))
	if err != nil {
		return nil, fmt.Errorf("open checksums: %w", err)
	}
	defer file.Close()
	checksums := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || filepath.Base(fields[1]) != fields[1] {
			return nil, fmt.Errorf("invalid checksum line %q", scanner.Text())
		}
		actual, digestErr := fileSHA256(filepath.Join(directory, fields[1]))
		if digestErr != nil {
			return nil, digestErr
		}
		if actual != fields[0] {
			return nil, fmt.Errorf("checksum mismatch for %s", fields[1])
		}
		checksums[fields[1]] = fields[0]
	}
	if err = scanner.Err(); err != nil {
		return nil, fmt.Errorf("read checksums: %w", err)
	}
	if len(checksums) == 0 {
		return nil, fmt.Errorf("checksums file is empty")
	}
	return checksums, nil
}

func (p *Pipeline) verifyGitHubRelease(ctx context.Context, checksums map[string]string) (githubRelease, error) {
	tag := "v" + p.Version
	output, err := p.runner.output(ctx, "gh", "release", "view", tag, "--repo", GitHubRepository, "--json", "tagName,url,isDraft,isPrerelease,assets")
	if err != nil {
		return githubRelease{}, err
	}
	var release githubRelease
	if err = json.Unmarshal([]byte(output), &release); err != nil {
		return githubRelease{}, fmt.Errorf("decode GitHub release: %w", err)
	}
	if release.TagName != tag || release.IsDraft || !release.IsPrerelease {
		return githubRelease{}, fmt.Errorf("GitHub release %s is not a published prerelease", tag)
	}
	expected := make([]string, 0, len(checksums)+1)
	for name := range checksums {
		expected = append(expected, name)
	}
	expected = append(expected, "checksums.txt")
	sort.Strings(expected)
	actual := make([]string, 0, len(release.Assets))
	for _, asset := range release.Assets {
		actual = append(actual, asset.Name)
	}
	sort.Strings(actual)
	if strings.Join(actual, "\n") != strings.Join(expected, "\n") {
		return githubRelease{}, fmt.Errorf("GitHub release assets differ: got %v, want %v", actual, expected)
	}
	download, err := os.MkdirTemp("", "molejo-release-download-")
	if err != nil {
		return githubRelease{}, fmt.Errorf("create release download directory: %w", err)
	}
	defer os.RemoveAll(download)
	if err = p.runner.run(ctx, nil, nil, "gh", "release", "download", tag, "--repo", GitHubRepository, "--dir", download, "--pattern", "*"); err != nil {
		return githubRelease{}, err
	}
	for name, digest := range checksums {
		actualDigest, digestErr := fileSHA256(filepath.Join(download, name))
		if digestErr != nil {
			return githubRelease{}, digestErr
		}
		if actualDigest != digest {
			return githubRelease{}, fmt.Errorf("downloaded GitHub asset %s has the wrong checksum", name)
		}
	}
	return release, nil
}

func verifyAnonymousOCI(ctx context.Context, client *http.Client, repository, reference, wantedDigest string) error {
	values := url.Values{}
	values.Set("service", "ghcr.io")
	values.Set("scope", "repository:"+repository+":pull")
	tokenRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ghcr.io/token?"+values.Encode(), nil)
	if err != nil {
		return err
	}
	tokenResponse, err := client.Do(tokenRequest)
	if err != nil {
		return fmt.Errorf("request anonymous GHCR token for %s: %w", repository, err)
	}
	defer tokenResponse.Body.Close()
	if tokenResponse.StatusCode != http.StatusOK {
		return responseError("request anonymous GHCR token for "+repository, tokenResponse)
	}
	var token struct {
		Token string `json:"token"`
	}
	if err = json.NewDecoder(tokenResponse.Body).Decode(&token); err != nil || token.Token == "" {
		return fmt.Errorf("decode anonymous GHCR token for %s", repository)
	}
	manifestURL := "https://ghcr.io/v2/" + repository + "/manifests/" + url.PathEscape(reference)
	manifestRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return err
	}
	manifestRequest.Header.Set("Authorization", "Bearer "+token.Token)
	manifestRequest.Header.Set("Accept", strings.Join([]string{
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.docker.distribution.manifest.v2+json",
	}, ", "))
	manifestResponse, err := client.Do(manifestRequest)
	if err != nil {
		return fmt.Errorf("pull anonymous GHCR manifest %s:%s: %w", repository, reference, err)
	}
	defer manifestResponse.Body.Close()
	if manifestResponse.StatusCode != http.StatusOK {
		return responseError("pull anonymous GHCR manifest "+repository+":"+reference, manifestResponse)
	}
	actualDigest := manifestResponse.Header.Get("Docker-Content-Digest")
	if wantedDigest != "" && actualDigest != wantedDigest {
		return fmt.Errorf("GHCR digest mismatch for %s:%s: got %s, want %s", repository, reference, actualDigest, wantedDigest)
	}
	return nil
}

func responseError(operation string, response *http.Response) error {
	contents, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
	return fmt.Errorf("%s: HTTP %s: %s", operation, response.Status, strings.TrimSpace(string(contents)))
}

func (p *Pipeline) verifyCharts(ctx context.Context, directory string, manifest Manifest, client *http.Client) error {
	temporary, err := os.MkdirTemp("", "molejo-helm-verify-")
	if err != nil {
		return fmt.Errorf("create Helm verification directory: %w", err)
	}
	defer os.RemoveAll(temporary)
	registryConfig := filepath.Join(temporary, "registry.json")
	if err = os.WriteFile(registryConfig, []byte("{\"auths\":{}}\n"), 0o600); err != nil {
		return fmt.Errorf("write empty Helm registry config: %w", err)
	}
	for _, chart := range Charts {
		artifact := manifest.Charts[chart.Name]
		if artifact.Digest == "" || artifact.File == "" {
			return fmt.Errorf("manifest is missing chart %s", chart.Name)
		}
		repository := "molejo-platform/charts/" + chart.Name
		if err = verifyAnonymousOCI(ctx, client, repository, p.Version, artifact.Digest); err != nil {
			return err
		}
		if err = p.runner.run(ctx, []string{"HELM_REGISTRY_CONFIG=" + registryConfig}, nil, "helm", "pull", ChartRegistry+"/"+chart.Name, "--version", p.Version, "--destination", temporary); err != nil {
			return err
		}
		localDigest, digestErr := fileSHA256(filepath.Join(directory, artifact.File))
		if digestErr != nil {
			return digestErr
		}
		remoteDigest, digestErr := fileSHA256(filepath.Join(temporary, artifact.File))
		if digestErr != nil {
			return digestErr
		}
		if localDigest != remoteDigest {
			return fmt.Errorf("downloaded chart %s differs from the local release artifact", chart.Name)
		}
	}
	return nil
}
