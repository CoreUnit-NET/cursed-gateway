package main

/*
Compare descriptor inputs to the cached fingerprint. Only when they change
(or --force): extract descriptors → protoc → validate.
*/

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	modulePath     = "github.com/CoreUnit-NET/cursed-gateway"
	agentHashFile  = "inputs.sha256"
	extractSubdir  = "extract"
	selectedSubdir = "selected"
)

var requiredSymbols = []string{
	"AgentClientMessage",
	"AgentServerMessage",
	"AgentService",
}

const (
	packageREPattern   = `(?m)^\s*package\s+([a-zA-Z0-9_.]+)\s*;`
	goPackageREPattern = `(?m)^\s*option\s+go_package\s*=\s*"[^"]*"\s*;`
)

// Run skips work when descriptor inputs match the last successful generation.
func Run(cfg *Config, deps *Deps) error {
	hashPath := filepath.Join(cfg.CacheDir, agentHashFile)
	rawDir := filepath.Join(cfg.CacheDir, extractSubdir, "raw")

	newHash, err := fingerprintInputs(deps)
	if err != nil {
		return err
	}

	if !cfg.Force && inputsUnchanged(hashPath, newHash) && hasGenerated(cfg.ProtoOut) {
		fmt.Println("Inputs unchanged — skipping extract/codegen")
		return validateGenerated(cfg.ProtoOut)
	}

	fmt.Println("Regenerating lib/cursorProto...")
	_ = os.RemoveAll(rawDir)
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		return err
	}

	fmt.Println("Extracting protobuf descriptors...")
	wrote, err := extractDescriptors(deps, rawDir)
	if err != nil {
		return err
	}
	if wrote == 0 {
		return fmt.Errorf("extracted 0 protobuf descriptors from agent %s", deps.AgentVer)
	}
	fmt.Printf("Extracted %d descriptor artifact(s)\n", wrote)

	fds, err := loadFileDescriptors(rawDir)
	if err != nil {
		return err
	}

	if len(fds) > 0 {
		if err := codegenFromDescriptors(cfg, deps, fds); err != nil {
			return err
		}
	} else {
		selectedDir := filepath.Join(cfg.CacheDir, extractSubdir, selectedSubdir)
		kept, err := selectAndRewrite(rawDir, selectedDir)
		if err != nil {
			return err
		}
		if len(kept) == 0 {
			return fmt.Errorf("no agent.v1 / aiserver.v1 protos found after filter")
		}
		fmt.Printf("Selected %d proto file(s)\n", len(kept))
		if err := codegen(cfg, deps, selectedDir, kept); err != nil {
			return err
		}
	}

	if err := validateGenerated(cfg.ProtoOut); err != nil {
		return err
	}
	if err := os.WriteFile(hashPath, []byte(newHash+"\n"), 0o644); err != nil {
		return err
	}
	return nil
}

// fingerprintInputs hashes the inputs that affect generated Go.
// Node agents currently codegen from the committed agent_pb.ts fallback
// (live JS has no FileDescriptorProto), keyed by agent version.
// Binary agents hash the scanned ELF binaries.
func fingerprintInputs(deps *Deps) (string, error) {
	h := sha256.New()
	fmt.Fprintf(h, "agent=%s\n", deps.AgentVer)
	if deps.NodeAgent {
		fmt.Fprintf(h, "kind=node\n")
		fallback := filepath.Join(findModuleRoot(), repoAgentPBTS)
		if err := hashNamedFile(h, fallback); err != nil {
			return "", fmt.Errorf("fallback descriptor source: %w", err)
		}
	} else {
		fmt.Fprintf(h, "kind=binary\n")
		for _, f := range deps.ScanFiles {
			if err := hashNamedFile(h, f); err != nil {
				return "", err
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashNamedFile(h io.Writer, path string) error {
	fmt.Fprintf(h, "file=%s\n", filepath.Base(path))
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(h, f)
	_ = f.Close()
	if copyErr != nil {
		return copyErr
	}
	_, err = h.Write([]byte{0})
	return err
}

func inputsUnchanged(hashPath, newHash string) bool {
	old, err := os.ReadFile(hashPath)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(old)) == newHash
}

func hasGenerated(protoOut string) bool {
	st, err := os.Stat(filepath.Join(protoOut, "agent.pb.go"))
	return err == nil && !st.IsDir()
}

func runProtodump(bin string, files []string, outDir string) (int, error) {
	before, _ := countProtoFiles(outDir)
	for _, file := range files {
		fmt.Printf("  protodump -file %s\n", filepath.Base(file))
		cmd := exec.Command(bin, "-file", file, "-output", outDir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("  warning: protodump failed on %s: %v\n", filepath.Base(file), err)
		}
	}
	after, err := countProtoFiles(outDir)
	if err != nil {
		return 0, err
	}
	return after - before, nil
}

func countProtoFiles(dir string) (int, error) {
	n := 0
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".proto") {
			n++
		}
		return nil
	})
	return n, err
}

func selectAndRewrite(rawDir, selectedDir string) ([]string, error) {
	packageRE, err := regexp.Compile(packageREPattern)
	if err != nil {
		return nil, fmt.Errorf("package regexp: %w", err)
	}
	goPackageRE, err := regexp.Compile(goPackageREPattern)
	if err != nil {
		return nil, fmt.Errorf("go_package regexp: %w", err)
	}

	_ = os.RemoveAll(selectedDir)
	if err := os.MkdirAll(selectedDir, 0o755); err != nil {
		return nil, err
	}
	var kept []string
	err = filepath.WalkDir(rawDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".proto") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		pkg := ""
		if m := packageRE.FindSubmatch(data); m != nil {
			pkg = string(m[1])
		}
		if !wantPackage(pkg, data) {
			return nil
		}
		rel, err := filepath.Rel(rawDir, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(selectedDir, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, injectGoPackage(data, pkg, rel, goPackageRE), 0o644); err != nil {
			return err
		}
		kept = append(kept, rel)
		return nil
	})
	return kept, err
}

func wantPackage(pkg string, data []byte) bool {
	switch {
	case pkg == "agent.v1", strings.HasPrefix(pkg, "agent.v1"):
		return true
	case pkg == "aiserver.v1", strings.HasPrefix(pkg, "aiserver.v1"):
		return true
	case strings.HasPrefix(pkg, "google.protobuf"):
		return true
	}
	for _, sym := range requiredSymbols {
		if len(data) > 0 && bytes.Contains(data, []byte(sym)) {
			return true
		}
	}
	return false
}

func injectGoPackage(data []byte, pkg, rel string, goPackageRE *regexp.Regexp) []byte {
	goPkg := goPackageFor(pkg, rel)
	optionLine := fmt.Sprintf("option go_package = %q;\n", goPkg)
	if goPackageRE.Match(data) {
		return goPackageRE.ReplaceAll(data, []byte(optionLine))
	}
	lines := strings.SplitAfter(string(data), "\n")
	var out strings.Builder
	inserted := false
	for i, line := range lines {
		out.WriteString(line)
		trim := strings.TrimSpace(line)
		if !inserted && (strings.HasPrefix(trim, "package ") || strings.HasPrefix(trim, "syntax ")) {
			if strings.HasPrefix(trim, "package ") {
				out.WriteString(optionLine)
				inserted = true
			} else if i+1 >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[i+1]), "package ") {
				out.WriteString(optionLine)
				inserted = true
			}
		}
	}
	if !inserted {
		return append([]byte(optionLine), data...)
	}
	return []byte(out.String())
}

func goPackageFor(pkg, rel string) string {
	_ = pkg
	_ = rel
	return modulePath + "/lib/cursorProto;cursorProto"
}

func codegen(cfg *Config, deps *Deps, selectedDir string, rels []string) error {
	_ = os.RemoveAll(cfg.ProtoOut)
	if err := os.MkdirAll(cfg.ProtoOut, 0o755); err != nil {
		return err
	}
	args := []string{
		"--proto_path=" + selectedDir,
		"--go_out=" + cfg.ProtoOut,
		"--go_opt=paths=source_relative",
	}
	for _, rel := range rels {
		args = append(args, filepath.Join(selectedDir, rel))
	}
	fmt.Printf("Running protoc on %d file(s)...\n", len(rels))
	cmd := exec.Command(deps.Protoc, args...)
	cmd.Env = append(os.Environ(), "PATH="+deps.PluginDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("protoc: %w", err)
	}
	return nil
}

func validateGenerated(protoOut string) error {
	agentGo := filepath.Join(protoOut, "agent.pb.go")
	data, err := os.ReadFile(agentGo)
	if err != nil {
		return fmt.Errorf("validation failed: expected %s: %w", agentGo, err)
	}
	pkgRE, err := regexp.Compile(`(?m)^package\s+cursorProto\s*$`)
	if err != nil {
		return fmt.Errorf("validation failed: package regexp: %w", err)
	}
	if !pkgRE.Match(data) {
		return fmt.Errorf("validation failed: %s must declare package cursorProto", agentGo)
	}
	pbFiles := []string{agentGo}
	_ = filepath.WalkDir(protoOut, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == agentGo {
			return err
		}
		if strings.HasSuffix(d.Name(), ".pb.go") {
			pbFiles = append(pbFiles, path)
		}
		return nil
	})
	for _, sym := range requiredSymbols {
		if !symbolPresent(pbFiles, sym) {
			return fmt.Errorf("validation failed: missing %s in generated Go", sym)
		}
	}
	cmd := exec.Command("go", "test", ".")
	cmd.Dir = protoOut
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("validation failed: generated package does not compile: %w", err)
	}
	fmt.Printf("Validation OK (%s, package cursorProto)\n", agentGo)
	return nil
}

func symbolPresent(files []string, sym string) bool {
	reType, err := regexp.Compile(`type\s+` + regexp.QuoteMeta(sym) + `\b`)
	if err != nil {
		return false
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if reType.Match(data) || (sym == "AgentService" && bytes.Contains(data, []byte("AgentService"))) {
			return true
		}
	}
	return false
}

func findModuleRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return wd
		}
		dir = parent
	}
}
