package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const defaultAPIBaseURL = "https://api.github.com"

type Updater struct {
	client     *http.Client
	apiBaseURL string
	owner      string
	repo       string
}

type CheckResult struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
}

type ApplyResult struct {
	PreviousVersion string
	Version         string
}

type release struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func NewUpdater(owner, repo string) *Updater {
	return &Updater{
		client:     http.DefaultClient,
		apiBaseURL: defaultAPIBaseURL,
		owner:      owner,
		repo:       repo,
	}
}

func (u *Updater) Check(currentVersion string) (CheckResult, error) {
	current, err := normalizeVersion(currentVersion)
	if err != nil {
		return CheckResult{}, err
	}

	rel, err := u.latestRelease()
	if err != nil {
		return CheckResult{}, err
	}

	latest, err := normalizeVersion(rel.TagName)
	if err != nil {
		return CheckResult{}, fmt.Errorf("parsing latest release version: %w", err)
	}

	return CheckResult{
		CurrentVersion:  current,
		LatestVersion:   latest,
		UpdateAvailable: compareVersions(latest, current) > 0,
	}, nil
}

func (u *Updater) Apply(currentVersion string, executablePath string) (ApplyResult, error) {
	current, err := normalizeVersion(currentVersion)
	if err != nil {
		return ApplyResult{}, err
	}

	info, err := os.Stat(executablePath)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("stat executable: %w", err)
	}
	if info.IsDir() {
		return ApplyResult{}, fmt.Errorf("executable path %s is directory", executablePath)
	}

	rel, err := u.latestRelease()
	if err != nil {
		return ApplyResult{}, err
	}

	latest, err := normalizeVersion(rel.TagName)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("parsing latest release version: %w", err)
	}
	if compareVersions(latest, current) <= 0 {
		return ApplyResult{}, fmt.Errorf("muxedo %s already up to date", current)
	}

	archiveName := archiveFilename(latest, runtime.GOOS, runtime.GOARCH)
	archiveAsset, err := findAsset(rel.Assets, archiveName)
	if err != nil {
		return ApplyResult{}, err
	}

	checksumAsset, err := findAsset(rel.Assets, "checksums.txt")
	if err != nil {
		return ApplyResult{}, err
	}

	expectedHash, err := u.fetchChecksum(checksumAsset.BrowserDownloadURL, archiveName)
	if err != nil {
		return ApplyResult{}, err
	}

	binaryData, err := u.fetchAndExtractBinary(archiveAsset.BrowserDownloadURL, archiveName, expectedHash)
	if err != nil {
		return ApplyResult{}, err
	}

	if err := replaceExecutable(executablePath, binaryData, info.Mode()); err != nil {
		return ApplyResult{}, err
	}

	return ApplyResult{
		PreviousVersion: current,
		Version:         latest,
	}, nil
}

func (u *Updater) latestRelease() (release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", strings.TrimRight(u.apiBaseURL, "/"), u.owner, u.repo)
	resp, err := u.get(url)
	if err != nil {
		return release{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return release{}, readHTTPError("fetching latest release", resp)
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return release{}, fmt.Errorf("decoding latest release response: %w", err)
	}
	return rel, nil
}

func (u *Updater) fetchChecksum(url string, archiveName string) (string, error) {
	resp, err := u.get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", readHTTPError("fetching checksums", resp)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading checksums: %w", err)
	}

	sum, err := parseChecksumFile(string(data), archiveName)
	if err != nil {
		return "", err
	}
	return sum, nil
}

func (u *Updater) fetchAndExtractBinary(url string, archiveName string, expectedHash string) ([]byte, error) {
	resp, err := u.get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readHTTPError("downloading release asset", resp)
	}

	archiveData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", archiveName, err)
	}

	actualHash := sha256.Sum256(archiveData)
	if hex.EncodeToString(actualHash[:]) != expectedHash {
		return nil, fmt.Errorf("checksum mismatch for %s", archiveName)
	}

	binaryData, err := extractBinaryFromTarGz(archiveData)
	if err != nil {
		return nil, err
	}
	return binaryData, nil
}

func (u *Updater) get(url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "muxedo-updater")

	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting %s: %w", url, err)
	}
	return resp, nil
}

func normalizeVersion(version string) (string, error) {
	if version == "dev" {
		return "", fmt.Errorf("self-update unsupported for dev builds")
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return "", fmt.Errorf("empty version")
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	if _, err := parseVersion(version); err != nil {
		return "", err
	}
	return version, nil
}

func compareVersions(left string, right string) int {
	lv, _ := parseVersion(left)
	rv, _ := parseVersion(right)
	for i := range lv {
		if lv[i] < rv[i] {
			return -1
		}
		if lv[i] > rv[i] {
			return 1
		}
	}
	return 0
}

func parseVersion(version string) ([3]int, error) {
	var parsed [3]int
	if !strings.HasPrefix(version, "v") {
		return parsed, fmt.Errorf("invalid version %q", version)
	}
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(parts) != 3 {
		return parsed, fmt.Errorf("invalid version %q", version)
	}
	for i, part := range parts {
		if part == "" {
			return parsed, fmt.Errorf("invalid version %q", version)
		}
		value := 0
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return parsed, fmt.Errorf("invalid version %q", version)
			}
			value = value*10 + int(ch-'0')
		}
		parsed[i] = value
	}
	return parsed, nil
}

func archiveFilename(version string, goos string, goarch string) string {
	return fmt.Sprintf("muxedo_%s_%s_%s.tar.gz", strings.TrimPrefix(version, "v"), goos, goarch)
}

func findAsset(assets []releaseAsset, name string) (releaseAsset, error) {
	for _, asset := range assets {
		if asset.Name == name {
			return asset, nil
		}
	}
	return releaseAsset{}, fmt.Errorf("release asset %q not found", name)
}

func parseChecksumFile(data string, filename string) (string, error) {
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == filename {
			sum := strings.ToLower(fields[0])
			if len(sum) != 64 {
				return "", fmt.Errorf("invalid checksum for %s", filename)
			}
			for _, ch := range sum {
				if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
					return "", fmt.Errorf("invalid checksum for %s", filename)
				}
			}
			return sum, nil
		}
	}
	return "", fmt.Errorf("checksum for %s not found", filename)
}

func extractBinaryFromTarGz(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("opening tar.gz: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar.gz: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(header.Name) != "muxedo" {
			continue
		}
		binaryData, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("reading muxedo binary from archive: %w", err)
		}
		if len(binaryData) == 0 {
			return nil, fmt.Errorf("archive muxedo binary is empty")
		}
		return binaryData, nil
	}

	return nil, fmt.Errorf("muxedo binary not found in archive")
}

func replaceExecutable(path string, binaryData []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)

	tempFile, err := os.CreateTemp(dir, ".muxedo-update-*")
	if err != nil {
		return fmt.Errorf("creating temp file in executable directory: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := tempFile.Write(binaryData); err != nil {
		tempFile.Close()
		return fmt.Errorf("writing new executable: %w", err)
	}
	if err := tempFile.Chmod(mode | 0o111); err != nil {
		tempFile.Close()
		return fmt.Errorf("marking new executable: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("closing new executable: %w", err)
	}

	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replacing executable %s: %w", path, err)
	}
	return nil
}

func readHTTPError(action string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	message := strings.TrimSpace(string(body))
	if message == "" {
		return fmt.Errorf("%s: unexpected HTTP %s", action, resp.Status)
	}
	return fmt.Errorf("%s: unexpected HTTP %s: %s", action, resp.Status, message)
}
