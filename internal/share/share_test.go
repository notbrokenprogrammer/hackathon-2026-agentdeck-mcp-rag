package share

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestVerifiedConnectorInstall(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			binary := []byte("disposable test executable, never executed")
			payload := binary
			if goos == "darwin" {
				var archive bytes.Buffer
				gz := gzip.NewWriter(&archive)
				tw := tar.NewWriter(gz)
				tw.WriteHeader(&tar.Header{Name: "cloudflared", Mode: 0755, Size: int64(len(binary)), Typeflag: tar.TypeReg})
				tw.Write(binary)
				tw.Close()
				gz.Close()
				payload = archive.Bytes()
			}
			name, _ := assetName(goos, "arm64")
			sum := sha256.Sum256(payload)
			asset := releaseAsset{Name: name, URL: "https://github.com/cloudflare/cloudflared/releases/download/2026.10.2/" + name, Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: int64(len(payload))}
			badChecksum := false
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				var body []byte
				switch r.URL.String() {
				case releaseAPI:
					copy := asset
					if badChecksum {
						copy.Digest = "sha256:" + strings.Repeat("0", 64)
					}
					body, _ = json.Marshal(release{Tag: "2026.10.2", Assets: []releaseAsset{copy}})
				case asset.URL:
					body = payload
				default:
					t.Fatalf("unexpected URL %s", r.URL)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body))}, nil
			})}
			dir := t.TempDir()
			path, err := install(context.Background(), client, dir, goos, "arm64")
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(data, binary) {
				t.Fatal("verified executable contents", err)
			}
			before, _ := os.ReadFile(path)
			badChecksum = true
			_, err = install(context.Background(), client, dir, goos, "arm64")
			if err == nil {
				t.Fatal("unverified update installed")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("failed update changed prior binary")
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 1 {
				t.Fatal("download residue", entries)
			}
		})
	}
}
func TestConnectorURLAndArguments(t *testing.T) {
	if got := ParseURL("INF | https://clean-cobalt-crew.trycloudflare.com |"); got != "https://clean-cobalt-crew.trycloudflare.com" {
		t.Fatal(got)
	}
	for _, line := range []string{"https://evil.example", "https://trycloudflare.com", "http://clean-cobalt.trycloudflare.com", "https://clean-cobalt.trycloudflare.com.evil", "https://clean-cobalt.trycloudflare.com@evil.example", "https://clean-cobalt.trycloudflare.com-evil", "https://clean-cobalt.trycloudflare.com:443.evil", "https://clean-cobalt.trycloudflare.com/redirect"} {
		if ParseURL(line) != "" {
			t.Fatal("invalid public origin", line)
		}
	}
	if got := ParseURL(`{"url":"https://clean-cobalt.trycloudflare.com"}`); got != "https://clean-cobalt.trycloudflare.com" {
		t.Fatal("quoted connector URL", got)
	}
	args := Arguments("http://127.0.0.1:8080", "/tmp/disposable-config.yml")
	got := strings.Join(args, " ")
	if !strings.Contains(got, "--config /tmp/disposable-config.yml") || !strings.Contains(got, "--no-autoupdate") || !strings.Contains(got, "--protocol http2") {
		t.Fatal(got)
	}
}
func TestMain(m *testing.M) {
	if mode := os.Getenv("LASTCREW_CONNECTOR_TEST_CHILD"); mode != "" {
		if mode != "connection-only" && mode != "silent" {
			fmt.Fprintln(os.Stderr, "INF | https://clean-cobalt-crew.trycloudflare.com |")
		}
		if mode == "gated" || mode == "stall" {
			fmt.Fprintln(os.Stderr, "WRN test URL provisioned, no edge connection")
			if mode == "stall" {
				time.Sleep(10 * time.Second)
			} else {
				until := time.Now().Add(5 * time.Second)
				for time.Now().Before(until) {
					if _, err := os.Stat(os.Getenv("LASTCREW_CONNECTOR_TEST_RELEASE")); err == nil {
						break
					}
					time.Sleep(2 * time.Millisecond)
				}
			}
		}
		if mode == "1" || mode == "gated" || mode == "connection-only" {
			fmt.Fprintln(os.Stderr, "INF Registered tunnel connection connIndex=0 protocol=http2")
			fmt.Fprintln(os.Stdout, "INF Registered tunnel connection connIndex=0 protocol=http2")
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestConnectorProcessLifecycle(t *testing.T) {
	t.Setenv("LASTCREW_CONNECTOR_TEST_CHILD", "1")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	urls := make(chan string, 1)
	process, err := Start(ctx, binary, "http://127.0.0.1:8080", func(url string) { urls <- url }, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer process.Stop()
	select {
	case url := <-urls:
		if url != "https://clean-cobalt-crew.trycloudflare.com" {
			t.Fatal(url)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connector URL not delivered")
	}
	select {
	case err := <-process.Done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connector did not exit")
	}
	if len(urls) != 0 {
		t.Fatal("ready callback repeated")
	}
}

type connectorLog func([]byte)

func (f connectorLog) Write(p []byte) (int, error) { f(p); return len(p), nil }

func TestConnectorWaitsForRegisteredConnection(t *testing.T) {
	t.Setenv("LASTCREW_CONNECTOR_TEST_CHILD", "gated")
	release := filepath.Join(t.TempDir(), "allow-edge-registration")
	t.Setenv("LASTCREW_CONNECTOR_TEST_RELEASE", release)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	urls, provisioned := make(chan string, 2), make(chan struct{}, 1)
	log := connectorLog(func(p []byte) {
		if strings.Contains(string(p), "test URL provisioned") {
			provisioned <- struct{}{}
		}
	})
	process, err := start(ctx, binary, "http://127.0.0.1:8080", func(url string) { urls <- url }, log, 4*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer process.Stop()
	select {
	case <-provisioned:
	case <-time.After(3 * time.Second):
		t.Fatal("fixture did not provision its URL")
	}
	// This log is handled after the URL on the same stderr scanner, so the
	// assertion does not depend on a sleep or subprocess scheduling speed.
	select {
	case <-urls:
		t.Fatal("announced a provisioned URL before edge registration")
	default:
	}
	if err := os.WriteFile(release, []byte("register now"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case url := <-urls:
		if url != "https://clean-cobalt-crew.trycloudflare.com" {
			t.Fatal(url)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ready connection was not announced")
	}
	select {
	case err := <-process.Done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connector did not exit")
	}
	if len(urls) != 0 {
		t.Fatal("ready callback repeated")
	}
}

func TestConnectorExitBeforeReady(t *testing.T) {
	for _, mode := range []string{"url-only", "connection-only", "silent"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("LASTCREW_CONNECTOR_TEST_CHILD", mode)
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			urls := make(chan string, 1)
			process, err := Start(context.Background(), binary, "http://127.0.0.1:8080", func(url string) { urls <- url }, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			defer process.Stop()
			select {
			case err := <-process.Done:
				if err == nil || !strings.Contains(err.Error(), "before its public connection became ready") {
					t.Fatal("early successful process exit must fail readiness", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("early exit was not delivered")
			}
			if len(urls) != 0 {
				t.Fatal("incomplete startup published a URL")
			}
		})
	}
}

func TestConnectorWatchdogAfterURL(t *testing.T) {
	t.Setenv("LASTCREW_CONNECTOR_TEST_CHILD", "stall")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	urls, provisioned := make(chan string, 1), make(chan struct{}, 1)
	log := connectorLog(func(p []byte) {
		if strings.Contains(string(p), "test URL provisioned") {
			provisioned <- struct{}{}
		}
	})
	process, err := start(context.Background(), binary, "http://127.0.0.1:8080", func(url string) { urls <- url }, log, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer process.Stop()
	select {
	case <-provisioned:
	case <-time.After(2 * time.Second):
		t.Fatal("fixture URL was not observed")
	}
	select {
	case err := <-process.Done:
		if err == nil || !strings.Contains(err.Error(), "did not become ready") {
			t.Fatal("URL alone must not disable the watchdog", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watchdog did not stop the child")
	}
	if len(urls) != 0 {
		t.Fatal("stalled connector published a URL")
	}
}
func TestExplicitBinary(t *testing.T) {
	file := filepath.Join(t.TempDir(), BinaryName("darwin", "arm64"))
	if err := os.WriteFile(file, []byte("test"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLOUDFLARED_BINARY", file)
	found, err := FindBinary()
	if err != nil || found != file {
		t.Fatal(found, err)
	}
}
