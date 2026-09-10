package release

const (
	GitHubRepository = "molejo-platform/fruto"
	Registry         = "ghcr.io/molejo-platform"
	ChartRegistry    = "oci://ghcr.io/molejo-platform/charts"
)

type Image struct {
	Name        string
	Dockerfile  string
	Placeholder string
}

type Chart struct {
	Name string
	Path string
}

var Images = []Image{
	{Name: "platform-operator", Dockerfile: "services/platform-operator/Dockerfile", Placeholder: "ghcr.io/molejo-platform/platform-operator@sha256:" + zeroDigest},
	{Name: "cluster-agent", Dockerfile: "services/cluster-agent/Dockerfile", Placeholder: "ghcr.io/molejo-platform/cluster-agent@sha256:" + zeroDigest},
	{Name: "control-plane-api", Dockerfile: "services/control-plane-api/Dockerfile", Placeholder: "ghcr.io/molejo-platform/control-plane-api@sha256:" + zeroDigest},
	{Name: "console-web", Dockerfile: "apps/console-web/Dockerfile", Placeholder: "ghcr.io/molejo-platform/console-web@sha256:" + zeroDigest},
}

var Charts = []Chart{
	{Name: "molejo-cluster", Path: "deploy/charts/molejo-cluster"},
	{Name: "molejo-control-plane", Path: "deploy/charts/molejo-control-plane"},
}

var BinaryTargets = []struct {
	GOOS   string
	GOARCH string
}{
	{GOOS: "darwin", GOARCH: "arm64"},
	{GOOS: "darwin", GOARCH: "amd64"},
	{GOOS: "linux", GOARCH: "amd64"},
}

const zeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"
