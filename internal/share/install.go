package share

import (
	"archive/tar"
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
	"regexp"
	"strings"
	"time"
)

const releaseAPI = "https://api.github.com/repos/cloudflare/cloudflared/releases/latest"
const maxBinaryBytes = 128 << 20

type releaseAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}
type release struct {
	Tag    string         `json:"tag_name"`
	Body   string         `json:"body"`
	Assets []releaseAsset `json:"assets"`
}

func assetName(goos, arch string) (string, error) {
	if (goos != "darwin" && goos != "linux" && goos != "windows") || (arch != "amd64" && arch != "arm64") {
		return "", fmt.Errorf("unsupported connector platform %s/%s", goos, arch)
	}
	name := "cloudflared-" + goos + "-" + arch
	if goos == "darwin" {
		name += ".tgz"
	}
	if goos == "windows" {
		name += ".exe"
	}
	return name, nil
}

// Install fetches the official release into the project, verifies its published
// SHA-256 before extraction and execution, and does not install a system service.
func Install(ctx context.Context, dir, goos, arch string) (string, error) {
	client := &http.Client{Timeout: 3 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if r.URL.Scheme != "https" || len(via) > 5 {
			return fmt.Errorf("unsafe connector download redirect")
		}
		return nil
	}}
	return install(ctx, client, dir, goos, arch)
}
func install(ctx context.Context, client *http.Client, dir, goos, arch string) (string, error) {
	name, err := assetName(goos, arch)
	if err != nil {
		return "", err
	}
	get := func(address string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "LastCrew-HTTPS-Installer")
		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			return nil, fmt.Errorf("connector download: HTTP %d", res.StatusCode)
		}
		return res, nil
	}
	res, err := get(releaseAPI)
	if err != nil {
		return "", err
	}
	var meta release
	err = json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&meta)
	res.Body.Close()
	if err != nil {
		return "", err
	}
	if !regexp.MustCompile(`^[0-9]{4}\.[0-9]+\.[0-9]+$`).MatchString(meta.Tag) {
		return "", fmt.Errorf("unexpected official release tag")
	}
	var asset releaseAsset
	for _, candidate := range meta.Assets {
		if candidate.Name == name {
			asset = candidate
			break
		}
	}
	if asset.Name == "" || asset.Size <= 0 || asset.Size > maxBinaryBytes {
		return "", fmt.Errorf("official release has no supported %s asset", name)
	}
	expectedURL := "https://github.com/cloudflare/cloudflared/releases/download/" + meta.Tag + "/" + name
	if asset.URL != expectedURL {
		return "", fmt.Errorf("unexpected connector download source")
	}
	digest := strings.TrimPrefix(asset.Digest, "sha256:")
	if !regexp.MustCompile(`^[a-fA-F0-9]{64}$`).MatchString(digest) {
		match := regexp.MustCompile(regexp.QuoteMeta(name) + `:\s*([a-fA-F0-9]{64})`).FindStringSubmatch(meta.Body)
		if len(match) != 2 {
			return "", fmt.Errorf("release does not publish a SHA-256 for %s", name)
		}
		digest = match[1]
	}
	if err = os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	archive, err := os.CreateTemp(dir, ".cloudflared-download-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	res, err = get(asset.URL)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(archive, hash), io.LimitReader(res.Body, asset.Size+1))
	res.Body.Close()
	if err != nil {
		return "", err
	}
	if n != asset.Size || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), digest) {
		return "", fmt.Errorf("connector checksum/size mismatch; executable was not installed")
	}
	if _, err = archive.Seek(0, 0); err != nil {
		return "", err
	}
	var input io.Reader = archive
	if goos == "darwin" {
		gz, err := gzip.NewReader(archive)
		if err != nil {
			return "", err
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		found := false
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}
			if filepath.Base(header.Name) == "cloudflared" && header.Typeflag == tar.TypeReg && header.Size > 0 && header.Size <= maxBinaryBytes {
				input = io.LimitReader(tr, header.Size)
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("verified archive contains no cloudflared executable")
		}
	}
	file, err := os.CreateTemp(dir, ".cloudflared-ready-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	_, err = io.Copy(file, io.LimitReader(input, maxBinaryBytes+1))
	if err == nil {
		err = file.Chmod(0755)
	}
	closeErr := file.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	target := filepath.Join(dir, BinaryName(goos, arch))
	if err = os.Rename(file.Name(), target); err != nil {
		return "", err
	}
	return filepath.Abs(target)
}
