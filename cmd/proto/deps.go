package main

/*
Install toolchain binaries into PROTO_CACHE_DIR and resolve the local
cursor-agent package. No remote agent download — use a local install.
*/

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	protocVersion  = "28.1"
	protodumpPkg   = "github.com/arkadiyt/protodump/cmd/protodump@latest"
	protocGenGoPkg = "google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.5"
	httpTimeout    = 120 * time.Second
	toolsSubdir    = "tools"
)

// Deps holds resolved tool paths and the source agent package.
type Deps struct {
	Protodump string
	Protoc    string
	PluginDir string
	AgentDir  string
	AgentVer  string   // directory basename, e.g. 2026.07.16-899851b
	ScanFiles []string // absolute paths under the source agent install
	NodeAgent bool
}

// Ensure installs tools into cache and resolves the local agent package.
func Ensure(cfg *Config) (*Deps, error) {
	cacheDir, err := filepath.Abs(cfg.CacheDir)
	if err != nil {
		return nil, fmt.Errorf("cache dir: %w", err)
	}
	cfg.CacheDir = cacheDir
	if err := os.MkdirAll(cfg.CacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("cache dir: %w", err)
	}
	if abs, err := filepath.Abs(cfg.ProtoOut); err == nil {
		cfg.ProtoOut = abs
	}

	toolsDir := filepath.Join(cfg.CacheDir, toolsSubdir)
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		return nil, err
	}

	pluginDir := filepath.Join(toolsDir, "go-bin")
	if _, err := ensureGoInstall(cfg.CacheDir, toolsDir, "protoc-gen-go", protocGenGoPkg); err != nil {
		return nil, fmt.Errorf("protoc-gen-go: %w", err)
	}
	protocBin, err := ensureProtoc(toolsDir)
	if err != nil {
		return nil, fmt.Errorf("protoc: %w", err)
	}

	agentDir, singleFile, err := resolveLocalAgent(cfg.AgentBin)
	if err != nil {
		return nil, fmt.Errorf("agent-bin: %w", err)
	}

	var scanFiles []string
	if singleFile != "" {
		scanFiles = []string{singleFile}
	} else {
		scanFiles, err = discoverScanTargets(agentDir)
		if err != nil {
			return nil, err
		}
	}
	if len(scanFiles) == 0 {
		return nil, fmt.Errorf("no scan targets found under %s", agentDir)
	}

	nodeAgent := agentLooksLikeNodeAgent(scanFiles)

	// protodump is only needed for binary (non-Node) agents.
	protodumpBin := ""
	if !nodeAgent {
		protodumpBin, err = ensureGoInstall(cfg.CacheDir, toolsDir, "protodump", protodumpPkg)
		if err != nil {
			return nil, fmt.Errorf("protodump: %w", err)
		}
	}

	return &Deps{
		Protodump: protodumpBin,
		Protoc:    protocBin,
		PluginDir: pluginDir,
		AgentDir:  agentDir,
		AgentVer:  filepath.Base(agentDir),
		ScanFiles: scanFiles,
		NodeAgent: nodeAgent,
	}, nil
}

func ensureGoInstall(cacheDir, toolsDir, binName, pkg string) (string, error) {
	binDir := filepath.Join(toolsDir, "go-bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(binDir, binName)
	if st, err := os.Stat(out); err == nil && !st.IsDir() && st.Size() > 0 {
		return out, nil
	}
	modCache := filepath.Join(cacheDir, "gomod")
	goCache := filepath.Join(cacheDir, "gobuild")
	if err := os.MkdirAll(modCache, 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(goCache, 0o755); err != nil {
		return "", err
	}
	fmt.Printf("Installing %s...\n", binName)
	cmd := exec.Command("go", "install", pkg)
	cmd.Env = append(os.Environ(),
		"GOBIN="+binDir,
		"GOMODCACHE="+modCache,
		"GOCACHE="+goCache,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go install %s: %w", pkg, err)
	}
	if _, err := os.Stat(out); err != nil {
		return "", fmt.Errorf("go install succeeded but %s missing: %w", out, err)
	}
	return out, nil
}

func ensureProtoc(toolsDir string) (string, error) {
	dir := filepath.Join(toolsDir, "protoc-"+protocVersion)
	bin := filepath.Join(dir, "bin", "protoc")
	if st, err := os.Stat(bin); err == nil && !st.IsDir() {
		return bin, nil
	}
	zipName, err := protocZipName()
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("https://github.com/protocolbuffers/protobuf/releases/download/v%s/%s", protocVersion, zipName)
	fmt.Printf("Downloading protoc %s...\n", protocVersion)
	zipPath := filepath.Join(toolsDir, zipName)
	if err := downloadFile(url, zipPath); err != nil {
		return "", err
	}
	_ = os.RemoveAll(dir)
	if err := unzip(zipPath, dir); err != nil {
		return "", err
	}
	if err := os.Chmod(bin, 0o755); err != nil {
		return "", err
	}
	return bin, nil
}

func protocZipName() (string, error) {
	var osPart, archPart string
	switch runtime.GOOS {
	case "linux":
		osPart = "linux"
	case "darwin":
		osPart = "osx"
	default:
		return "", fmt.Errorf("no protoc build for GOOS=%s", runtime.GOOS)
	}
	switch runtime.GOARCH {
	case "amd64":
		archPart = "x86_64"
	case "arm64":
		archPart = "aarch_64"
	default:
		return "", fmt.Errorf("no protoc build for GOARCH=%s", runtime.GOARCH)
	}
	return fmt.Sprintf("protoc-%s-%s-%s.zip", protocVersion, osPart, archPart), nil
}

// resolveLocalAgent accepts a package dir, versions dir / share root, or a file.
func resolveLocalAgent(path string) (agentDir, singleFile string, err error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}
	if !st.IsDir() {
		return filepath.Dir(path), path, nil
	}
	if isAgentPackageDir(path) {
		return path, "", nil
	}
	versionsDir := path
	if filepath.Base(path) != "versions" {
		candidate := filepath.Join(path, "versions")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			versionsDir = candidate
		}
	}
	newest, err := newestAgentPackage(versionsDir)
	if err != nil {
		return "", "", err
	}
	fmt.Printf("Using local cursor-agent %s\n", filepath.Base(newest))
	return newest, "", nil
}

func isAgentPackageDir(dir string) bool {
	for _, m := range []string{"index.js", "cursor-agent", "cursor-agent-svc.js", "package.json"} {
		if st, err := os.Stat(filepath.Join(dir, m)); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

func newestAgentPackage(versionsDir string) (string, error) {
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		return "", err
	}
	var bestDir, bestVer string
	var bestTime time.Time
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		cand := filepath.Join(versionsDir, e.Name())
		if !isAgentPackageDir(cand) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if bestDir == "" || info.ModTime().After(bestTime) || (info.ModTime().Equal(bestTime) && e.Name() > bestVer) {
			bestDir = cand
			bestVer = e.Name()
			bestTime = info.ModTime()
		}
	}
	if bestDir == "" {
		return "", fmt.Errorf("no cursor-agent packages under %s", versionsDir)
	}
	return bestDir, nil
}

// discoverScanTargets picks a small set of files worth scanning.
// Node agents: just the JS entrypoints. Binary agents: ELF executables.
func discoverScanTargets(agentDir string) ([]string, error) {
	var js []string
	for _, name := range []string{"index.js", "cursor-agent-svc.js"} {
		p := filepath.Join(agentDir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			js = append(js, p)
		}
	}
	if len(js) > 0 {
		return js, nil
	}

	var elfs []string
	err := filepath.WalkDir(agentDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == "node_modules" || name == ".running" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if isLikelyELFExecutable(path, info) {
			elfs = append(elfs, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	const maxTargets = 8
	if len(elfs) > maxTargets {
		elfs = elfs[:maxTargets]
	}
	return elfs, nil
}

func isLikelyELFExecutable(path string, info os.FileInfo) bool {
	if info.Mode()&0o111 == 0 && info.Size() < 100_000 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var hdr [4]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return false
	}
	return hdr[0] == 0x7f && hdr[1] == 'E' && hdr[2] == 'L' && hdr[3] == 'F'
}

func downloadFile(url, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	_ = os.Remove(tmp)
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, dest)
}

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	cleanDest := filepath.Clean(dest) + string(os.PathSeparator)
	for _, f := range r.File {
		target := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), cleanDest) && filepath.Clean(target) != filepath.Clean(dest) {
			return fmt.Errorf("invalid zip path %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		cerr := out.Close()
		if err != nil {
			return err
		}
		if cerr != nil {
			return cerr
		}
	}
	return nil
}
