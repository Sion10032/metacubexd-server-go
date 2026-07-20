// Package kernel downloads mihomo release binaries and the geo data files
// mihomo needs at runtime. Used by /api/control/geo/update and (eventually)
// a kernel-version switcher; for now geo is the only wired route.
//
// Direct port of packages/agent/src/kernel/{geo.ts, fetch-kernel.ts}.
package kernel

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// GEOAssetURLs are the canonical download sources for mihomo's default geo
// data. All three ship from meta-rules-dat's rolling `latest` release, which
// is the source mihomo documents for its default geodata.
var GEOAssetURLs = map[string]string{
	"geoip.dat":     "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.dat",
	"geosite.dat":   "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat",
	"country.mmdb":  "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/country.mmdb",
}

// geoFileOrder makes the written-files list deterministic across runs;
// iterating a map would yield insertion-dependent ordering.
var geoFileOrder = []string{"geoip.dat", "geosite.dat", "country.mmdb"}

// FetchGeoAssets downloads the three geo files into destDir (the kernel's
// home dir, so mihomo finds them at -d). Returns the list of written file
// names. A non-2xx response on any file aborts with a clear error.
func FetchGeoAssets(ctx context.Context, destDir string, hc *http.Client) ([]string, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Minute}
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	written := make([]string, 0, len(geoFileOrder))
	for _, file := range geoFileOrder {
		url := GEOAssetURLs[file]
		if err := downloadTo(ctx, hc, url, filepath.Join(destDir, file), 0o644); err != nil {
			return nil, fmt.Errorf("fetchGeoAssets: %w", err)
		}
		written = append(written, file)
	}
	return written, nil
}

// MihomoAsset describes one downloadable kernel binary: where to get it and
// how to unpack it (.gz gunzip, .zip extract-entry).
type MihomoAsset struct {
	Name    string // versioned file name, e.g. "mihomo-linux-amd64-compatible-v1.19.27.gz"
	URL     string
	Ext     string // "gz" or "zip"
	BinName string // "mihomo" or "mihomo.exe"
	// ZipEntry is the path inside the .zip for Windows archives (the inner
	// name drops the version). Empty for .gz.
	ZipEntry string
}

// MihomoAssetFor returns the release asset descriptor for the given os/arch.
// Variants follow mihomo's release naming: amd64 gets "-compatible" (runs on
// more CPUs), arm64 plain. Windows ships .zip; everything else .gz.
func MihomoAssetFor(osName, arch, version string) (MihomoAsset, error) {
	o, ok := osMap[osName]
	if !ok {
		return MihomoAsset{}, fmt.Errorf("unsupported os: %s", osName)
	}
	a, ok := archMap[arch]
	if !ok {
		return MihomoAsset{}, fmt.Errorf("unsupported arch: %s", arch)
	}
	ext := "gz"
	binName := "mihomo"
	if o == "windows" {
		ext = "zip"
		binName = "mihomo.exe"
	}
	variant := ""
	if a == "amd64" {
		variant = "-compatible"
	}
	name := fmt.Sprintf("mihomo-%s-%s%s-%s.%s", o, a, variant, version, ext)
	asset := MihomoAsset{
		Name:    name,
		URL:     fmt.Sprintf("https://github.com/MetaCubeX/mihomo/releases/download/%s/%s", version, name),
		Ext:     ext,
		BinName: binName,
	}
	if ext == "zip" {
		asset.ZipEntry = fmt.Sprintf("mihomo-%s-%s%s.exe", o, a, variant)
	}
	return asset, nil
}

var osMap = map[string]string{
	"linux":   "linux",
	"darwin":  "darwin",
	"windows": "windows",
}

var archMap = map[string]string{
	"x64":    "amd64",
	"amd64":  "amd64",
	"arm64":  "arm64",
	"aarch64": "arm64",
}

// FetchKernel downloads and installs the mihomo binary for the host's os/arch
// into destDir. .gz is gunzipped (single-file, never a tar); .zip is left to
// a future implementation (this build is Linux-only per GO_SERVER_PLAN.md §2
// 砍项 — Windows tree-kill / zip extraction is cut).
//
// Returns the absolute path to the installed binary.
func FetchKernel(ctx context.Context, destDir, version string, hc *http.Client) (string, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Minute}
	}
	if version == "" {
		return "", errors.New("fetchKernel: version is required")
	}
	asset, err := MihomoAssetFor(runtime.GOOS, runtime.GOARCH, version)
	if err != nil {
		return "", err
	}
	if asset.Ext == "zip" {
		return "", fmt.Errorf("fetchKernel: .zip extraction not implemented in this build (asset %s)", asset.Name)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	binPath := filepath.Join(destDir, asset.BinName)
	// Stream through gunzip so a large mihomo release doesn't sit in memory
	// twice (downloaded bytes + decompressed bytes).
	if err := downloadGunzipTo(ctx, hc, asset.URL, binPath); err != nil {
		return "", fmt.Errorf("fetchKernel: %w", err)
	}
	// 0o755: mihomo must be executable; group/other get rx so a different
	// runtime UID can still spawn it (container sometimes drops to nobody).
	if err := os.Chmod(binPath, 0o755); err != nil {
		return "", err
	}
	return binPath, nil
}

// downloadTo fetches a URL into dstPath verbatim (no decompression).
func downloadTo(ctx context.Context, hc *http.Client, url, dstPath string, mode os.FileMode) error {
	resp, err := hc.Get(url)
	if err != nil {
		return fmt.Errorf("download failed %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download returned %d for %s", resp.StatusCode, url)
	}
	return atomicWrite(dstPath, resp.Body, mode)
}

// downloadGunzipTo streams a gzip-compressed body through gunzip into dstPath.
// Used for mihomo's .gz release artifacts.
func downloadGunzipTo(ctx context.Context, hc *http.Client, url, dstPath string) error {
	resp, err := hc.Get(url)
	if err != nil {
		return fmt.Errorf("download failed %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download returned %d for %s", resp.StatusCode, url)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("gunzip init: %w", err)
	}
	defer gz.Close()
	return atomicWrite(dstPath, gz, 0o755)
}

// atomicWrite streams r into a temp file then renames, so a partial download
// can't leave a half-written binary that the supervisor might try to spawn.
func atomicWrite(dstPath string, r io.Reader, mode os.FileMode) error {
	tmp := dstPath + ".download.tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dstPath)
}
