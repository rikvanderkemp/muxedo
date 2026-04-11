package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCheckReportsUpdateAvailable(t *testing.T) {
	updater := newTestUpdater(t, "v1.2.4", archiveFilename("v1.2.4", runtime.GOOS, runtime.GOARCH), []byte("binary"))

	result, err := updater.Check("v1.2.3")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !result.UpdateAvailable {
		t.Fatal("Check() UpdateAvailable = false, want true")
	}
	if result.CurrentVersion != "v1.2.3" {
		t.Fatalf("Check() CurrentVersion = %q", result.CurrentVersion)
	}
	if result.LatestVersion != "v1.2.4" {
		t.Fatalf("Check() LatestVersion = %q", result.LatestVersion)
	}
}

func TestCheckRejectsDevBuild(t *testing.T) {
	updater := newTestUpdater(t, "v1.2.4", archiveFilename("v1.2.4", runtime.GOOS, runtime.GOARCH), []byte("binary"))

	_, err := updater.Check("dev")
	if err == nil || !strings.Contains(err.Error(), "dev builds") {
		t.Fatalf("Check() error = %v, want dev-build error", err)
	}
}

func TestApplyReplacesExecutable(t *testing.T) {
	updater := newTestUpdater(t, "v1.2.4", archiveFilename("v1.2.4", runtime.GOOS, runtime.GOARCH), []byte("new-binary"))

	dir := t.TempDir()
	execPath := filepath.Join(dir, "muxedo")
	if err := os.WriteFile(execPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := updater.Apply("v1.2.3", execPath)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.PreviousVersion != "v1.2.3" || result.Version != "v1.2.4" {
		t.Fatalf("Apply() result = %#v", result)
	}

	data, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "new-binary" {
		t.Fatalf("updated executable = %q, want new-binary", string(data))
	}
}

func TestApplyRejectsChecksumMismatch(t *testing.T) {
	archiveName := archiveFilename("v1.2.4", runtime.GOOS, runtime.GOARCH)
	archiveData := mustTarGz(t, []byte("new-binary"))

	updater := &Updater{
		client: newTestClient(map[string]testResponse{
			"https://example.test/repos/owner/repo/releases/latest": {
				statusCode: http.StatusOK,
				body:       fmt.Sprintf(`{"tag_name":"v1.2.4","assets":[{"name":"%s","browser_download_url":"https://example.test/archive"},{"name":"checksums.txt","browser_download_url":"https://example.test/checksums"}]}`, archiveName),
			},
			"https://example.test/archive": {
				statusCode: http.StatusOK,
				body:       string(archiveData),
			},
			"https://example.test/checksums": {
				statusCode: http.StatusOK,
				body:       fmt.Sprintf("%s  %s\n", strings.Repeat("a", 64), archiveName),
			},
		}),
		apiBaseURL: "https://example.test",
		owner:      "owner",
		repo:       "repo",
	}

	execPath := filepath.Join(t.TempDir(), "muxedo")
	if err := os.WriteFile(execPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := updater.Apply("v1.2.3", execPath)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Apply() error = %v, want checksum mismatch", err)
	}
}

func TestApplyRejectsMissingArchiveBinary(t *testing.T) {
	archiveName := archiveFilename("v1.2.4", runtime.GOOS, runtime.GOARCH)
	archiveData := mustTarGzNamed(t, "not-muxedo", []byte("new-binary"))
	sum := sha256.Sum256(archiveData)

	updater := &Updater{
		client: newTestClient(map[string]testResponse{
			"https://example.test/repos/owner/repo/releases/latest": {
				statusCode: http.StatusOK,
				body:       fmt.Sprintf(`{"tag_name":"v1.2.4","assets":[{"name":"%s","browser_download_url":"https://example.test/archive"},{"name":"checksums.txt","browser_download_url":"https://example.test/checksums"}]}`, archiveName),
			},
			"https://example.test/archive": {
				statusCode: http.StatusOK,
				body:       string(archiveData),
			},
			"https://example.test/checksums": {
				statusCode: http.StatusOK,
				body:       fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), archiveName),
			},
		}),
		apiBaseURL: "https://example.test",
		owner:      "owner",
		repo:       "repo",
	}

	execPath := filepath.Join(t.TempDir(), "muxedo")
	if err := os.WriteFile(execPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := updater.Apply("v1.2.3", execPath)
	if err == nil || !strings.Contains(err.Error(), "not found in archive") {
		t.Fatalf("Apply() error = %v, want missing binary error", err)
	}
}

func TestParseChecksumFileSupportsGNUFormat(t *testing.T) {
	got, err := parseChecksumFile("abc123\n0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef *muxedo_1.2.3_linux_amd64.tar.gz\n", "muxedo_1.2.3_linux_amd64.tar.gz")
	if err != nil {
		t.Fatalf("parseChecksumFile() error = %v", err)
	}
	want := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if got != want {
		t.Fatalf("parseChecksumFile() = %q, want %q", got, want)
	}
}

func newTestUpdater(t *testing.T, latestVersion string, archiveName string, binaryData []byte) *Updater {
	t.Helper()

	archiveData := mustTarGz(t, binaryData)
	sum := sha256.Sum256(archiveData)

	return &Updater{
		client: newTestClient(map[string]testResponse{
			"https://example.test/repos/owner/repo/releases/latest": {
				statusCode: http.StatusOK,
				body:       fmt.Sprintf(`{"tag_name":"%s","assets":[{"name":"%s","browser_download_url":"https://example.test/archive"},{"name":"checksums.txt","browser_download_url":"https://example.test/checksums"}]}`, latestVersion, archiveName),
			},
			"https://example.test/archive": {
				statusCode: http.StatusOK,
				body:       string(archiveData),
			},
			"https://example.test/checksums": {
				statusCode: http.StatusOK,
				body:       fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), archiveName),
			},
		}),
		apiBaseURL: "https://example.test",
		owner:      "owner",
		repo:       "repo",
	}
}

type testResponse struct {
	statusCode int
	body       string
}

type testTransport struct {
	responses map[string]testResponse
}

func newTestClient(responses map[string]testResponse) *http.Client {
	return &http.Client{
		Transport: testTransport{responses: responses},
	}
}

func (t testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, ok := t.responses[req.URL.String()]
	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     http.StatusText(http.StatusNotFound),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("not found")),
			Request:    req,
		}, nil
	}
	if response.statusCode == 0 {
		response.statusCode = http.StatusOK
	}
	return &http.Response{
		StatusCode: response.statusCode,
		Status:     fmt.Sprintf("%d %s", response.statusCode, http.StatusText(response.statusCode)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(response.body)),
		Request:    req,
	}, nil
}

func mustTarGz(t *testing.T, data []byte) []byte {
	t.Helper()
	return mustTarGzNamed(t, "muxedo", data)
}

func mustTarGzNamed(t *testing.T, name string, data []byte) []byte {
	t.Helper()

	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	tw := tar.NewWriter(gz)

	if err := tw.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0o755,
		Size: int64(len(data)),
	}); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close(tar) error = %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("Close(gzip) error = %v", err)
	}

	return buffer.Bytes()
}
