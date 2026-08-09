package main

/*
Extract FileDescriptorProtos from Cursor artifacts:
  1) protodump against binaries / raw embeds
  2) @bufbuild fileDesc("base64...") strings in JS/TS
  3) repo descriptor dump (agent_pb.ts) when the local agent is Node-based
    and embeds agent.v1 runtime types but no FileDescriptorProto
*/

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

const (
	repoAgentPBTS = "context/cursor-openai-api/src/cursor/proto/agent_pb.ts"
)

func extractDescriptors(deps *Deps, scanFiles []string, rawDir string) (int, error) {
	wrote := 0

	// 1) classic protodump (works on Go-built agent binaries)
	n, err := runProtodump(deps.Protodump, scanFiles, rawDir)
	if err != nil {
		return 0, err
	}
	wrote += n

	// 2) fileDesc("...") embeds in scanned JS/TS
	for _, f := range scanFiles {
		n, err := writeFileDescProtos(f, rawDir)
		if err != nil {
			fmt.Printf("  warning: fileDesc extract %s: %v\n", f, err)
			continue
		}
		wrote += n
	}

	if wrote > 0 {
		return wrote, nil
	}

	// 3) Node agent fallback: Cursor ships runtime field lists, not FDs.
	//    Use the committed agent_pb.ts descriptor dump when the agent clearly
	//    contains agent.v1 (validated against required symbol names).
	if agentLooksLikeNodeAgent(scanFiles) {
		root := findModuleRoot()
		fallback := filepath.Join(root, repoAgentPBTS)
		if _, err := os.Stat(fallback); err != nil {
			return 0, fmt.Errorf("protodump/fileDesc found nothing, and fallback %s missing", fallback)
		}
		fmt.Printf("Node cursor-agent has no embedded FileDescriptorProto — using %s\n", repoAgentPBTS)
		n, err := writeFileDescProtos(fallback, rawDir)
		if err != nil {
			return 0, err
		}
		if n == 0 {
			return 0, fmt.Errorf("no fileDesc blobs in fallback %s", fallback)
		}
		return n, nil
	}

	return 0, nil
}

func agentLooksLikeNodeAgent(scanFiles []string) bool {
	for _, f := range scanFiles {
		base := filepath.Base(f)
		if base != "index.js" && base != "cursor-agent-svc.js" && !strings.HasSuffix(base, ".js") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), `typeName:"agent.v1.AgentClientMessage"`) ||
			strings.Contains(string(data), `typeName:"agent.v1.AgentService"`) {
			return true
		}
	}
	return false
}

func writeFileDescProtos(path, rawDir string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	blobs := extractFileDescBlobs(data)
	wrote := 0
	for _, blob := range blobs {
		var fd descriptorpb.FileDescriptorProto
		if err := proto.Unmarshal(blob, &fd); err != nil {
			continue
		}
		name := fd.GetName()
		if name == "" || !strings.HasSuffix(name, ".proto") {
			continue
		}
		// Keep agent / aiserver / google only.
		pkg := fd.GetPackage()
		if !wantPackage(pkg, nil) && pkg != "" {
			// still allow by filename
			base := filepath.Base(name)
			if base != "agent.proto" && !strings.Contains(name, "aiserver") && !strings.Contains(name, "google/protobuf") {
				continue
			}
		}
		outName := sanitizeProtoPath(name)
		dest := filepath.Join(rawDir, outName)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return wrote, err
		}
		// Store raw FD bytes alongside; also emit via descriptor→proto text using protoc.
		if err := writeProtoFromFD(&fd, dest); err != nil {
			fmt.Printf("  warning: skip %s: %v\n", name, err)
			continue
		}
		fmt.Printf("  wrote %s from fileDesc\n", outName)
		wrote++
	}
	return wrote, nil
}

func sanitizeProtoPath(name string) string {
	name = strings.TrimPrefix(name, "/")
	name = filepath.Clean(name)
	if name == "." || strings.HasPrefix(name, "..") {
		return "unknown.proto"
	}
	return name
}

// writeProtoFromFD uses protoc to decode a FileDescriptorSet into .proto text.
func writeProtoFromFD(fd *descriptorpb.FileDescriptorProto, dest string) error {
	// Prefer regenerating source via a temporary descriptor set + protoc --decode_raw is useless.
	// Instead marshal FD and use the protobuf text format from protowire via a small helper:
	// We call `protoc --descriptor_set_in` only at codegen time; here write a minimal .proto
	// by asking protoc to dump? Not available.
	//
	// Practical approach: write the FileDescriptorProto bytes to dest+".fd" and a stub .proto
	// is NOT enough for selectAndRewrite.
	//
	// Use descriptor set file next to dest and let codegen consume FDs directly.
	return os.WriteFile(dest+".fd", mustMarshalFD(fd), 0o644)
}

func mustMarshalFD(fd *descriptorpb.FileDescriptorProto) []byte {
	b, err := proto.Marshal(fd)
	if err != nil {
		panic(err)
	}
	return b
}

// extractFileDescBlobs finds @bufbuild fileDesc("b64"[, "b64"...]) payloads.
func extractFileDescBlobs(data []byte) [][]byte {
	var out [][]byte
	s := string(data)
	for {
		i := strings.Index(s, "fileDesc(")
		if i < 0 {
			break
		}
		s = s[i+len("fileDesc("):]
		payload, rest, ok := readConcatStrings(s)
		s = rest
		if !ok || len(payload) < 16 {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			// try raw std without padding issues
			raw, err = base64.RawStdEncoding.DecodeString(payload)
			if err != nil {
				continue
			}
		}
		out = append(out, raw)
	}
	return out
}

// readConcatStrings parses one or more adjacent "..." / '...' string literals
// (optionally joined by + or commas) and returns concatenated content.
func readConcatStrings(s string) (payload string, rest string, ok bool) {
	var b strings.Builder
	found := false
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' || s[i] == ',' || s[i] == '+') {
			i++
		}
		if i >= len(s) {
			break
		}
		if s[i] == ')' {
			rest = s[i+1:]
			return b.String(), rest, found
		}
		quote := s[i]
		if quote != '"' && quote != '\'' {
			break
		}
		i++
		start := i
		for i < len(s) {
			if s[i] == '\\' {
				i += 2
				continue
			}
			if s[i] == quote {
				break
			}
			i++
		}
		if i >= len(s) {
			break
		}
		b.WriteString(s[start:i])
		found = true
		i++ // closing quote
	}
	if !found {
		return "", s, false
	}
	// consume through closing paren if present
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' || s[i] == ',') {
		i++
	}
	if i < len(s) && s[i] == ')' {
		i++
	}
	return b.String(), s[i:], true
}

func loadFileDescriptors(rawDir string) ([]*descriptorpb.FileDescriptorProto, error) {
	var out []*descriptorpb.FileDescriptorProto
	err := filepath.WalkDir(rawDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		switch {
		case strings.HasSuffix(d.Name(), ".fd"):
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var fd descriptorpb.FileDescriptorProto
			if err := proto.Unmarshal(data, &fd); err != nil {
				return nil
			}
			out = append(out, &fd)
		case strings.HasSuffix(d.Name(), ".proto"):
			// text protos from protodump — leave for selectAndRewrite path
		}
		return nil
	})
	return out, err
}

func injectGoPackageFD(fd *descriptorpb.FileDescriptorProto) {
	pkg := fd.GetPackage()
	rel := fd.GetName()
	goPkg := goPackageFor(pkg, rel)
	if fd.Options == nil {
		fd.Options = &descriptorpb.FileOptions{}
	}
	fd.Options.GoPackage = proto.String(goPkg)
	// Flat layout: lib/cursorProto/agent.pb.go (package cursorProto).
	switch {
	case pkg == "agent.v1" || strings.HasPrefix(pkg, "agent."):
		fd.Name = proto.String("agent.proto")
	case pkg == "aiserver.v1" || strings.HasPrefix(pkg, "aiserver."):
		fd.Name = proto.String("aiserver.proto")
	default:
		base := filepath.Base(fd.GetName())
		if base == "" || base == "." || base == string(filepath.Separator) {
			base = "unknown.proto"
		}
		fd.Name = proto.String(base)
	}
}

// fixNestedOneofCollisions renames messages that collide with protoc-gen-go
// oneof wrapper types. Cursor/buf descriptors often flatten nested messages to
// top-level "Parent_Child", which clashes with wrappers for oneof field "child".
func fixNestedOneofCollisions(fd *descriptorpb.FileDescriptorProto) {
	pkg := fd.GetPackage()
	byName := make(map[string]*descriptorpb.DescriptorProto, len(fd.MessageType))
	used := make(map[string]bool, len(fd.MessageType))
	for _, msg := range fd.MessageType {
		byName[msg.GetName()] = msg
		used[msg.GetName()] = true
	}

	type rename struct {
		msg     *descriptorpb.DescriptorProto
		oldName string
		newName string
	}
	var renames []rename

	for _, parent := range fd.MessageType {
		for _, field := range parent.Field {
			if field.OneofIndex == nil {
				continue
			}
			conflict := parent.GetName() + "_" + goCamelCase(field.GetName())
			victim := byName[conflict]
			if victim == nil {
				continue
			}
			newName := conflict + "Msg"
			for used[newName] {
				newName += "_"
			}
			used[newName] = true
			renames = append(renames, rename{msg: victim, oldName: conflict, newName: newName})
			delete(byName, conflict)
		}
	}

	for _, r := range renames {
		oldFull := dottedName(pkg, r.oldName)
		newFull := dottedName(pkg, r.newName)
		r.msg.Name = proto.String(r.newName)
		rewriteTypeNameRefs(fd, oldFull, newFull)
		fmt.Printf("  renamed %s -> %s (oneof/Go name collision)\n", oldFull, newFull)
	}

	// Also handle true nested DescriptorProto trees (non-flattened descriptors).
	for _, msg := range fd.MessageType {
		fixMessageOneofCollisions(fd, pkg, msg, []string{msg.GetName()})
	}
}

func dottedName(pkg, name string) string {
	if pkg == "" {
		return "." + name
	}
	return "." + pkg + "." + name
}

func fixMessageOneofCollisions(fd *descriptorpb.FileDescriptorProto, pkg string, msg *descriptorpb.DescriptorProto, path []string) {
	for _, nested := range msg.NestedType {
		childPath := append(append([]string{}, path...), nested.GetName())
		fixMessageOneofCollisions(fd, pkg, nested, childPath)
	}

	nestedByGo := map[string]*descriptorpb.DescriptorProto{}
	for _, nested := range msg.NestedType {
		nestedByGo[goCamelCase(nested.GetName())] = nested
	}

	type rename struct {
		nested  *descriptorpb.DescriptorProto
		oldName string
		newName string
	}
	var renames []rename
	used := map[string]bool{}
	for _, nested := range msg.NestedType {
		used[nested.GetName()] = true
	}

	for _, field := range msg.Field {
		if field.OneofIndex == nil {
			continue
		}
		goField := goCamelCase(field.GetName())
		nested := nestedByGo[goField]
		if nested == nil {
			continue
		}
		oldName := nested.GetName()
		newName := oldName + "Msg"
		for used[newName] {
			newName += "_"
		}
		used[newName] = true
		renames = append(renames, rename{nested: nested, oldName: oldName, newName: newName})
		delete(nestedByGo, goField)
	}

	for _, r := range renames {
		oldFull := fullProtoName(pkg, path, r.oldName)
		newFull := fullProtoName(pkg, path, r.newName)
		r.nested.Name = proto.String(r.newName)
		rewriteTypeNameRefs(fd, oldFull, newFull)
		fmt.Printf("  renamed nested %s -> %s (oneof/Go name collision)\n", oldFull, newFull)
	}
}

func fullProtoName(pkg string, parentPath []string, name string) string {
	parts := make([]string, 0, 1+len(parentPath)+1)
	if pkg != "" {
		parts = append(parts, pkg)
	}
	parts = append(parts, parentPath...)
	parts = append(parts, name)
	return "." + strings.Join(parts, ".")
}

func rewriteTypeNameRefs(fd *descriptorpb.FileDescriptorProto, oldFull, newFull string) {
	oldNoDot := strings.TrimPrefix(oldFull, ".")
	newNoDot := strings.TrimPrefix(newFull, ".")
	var walkMsg func(*descriptorpb.DescriptorProto)
	walkMsg = func(msg *descriptorpb.DescriptorProto) {
		for _, field := range msg.Field {
			rewriteTypeName(field, oldFull, newFull, oldNoDot, newNoDot)
		}
		for _, nested := range msg.NestedType {
			walkMsg(nested)
		}
	}
	for _, msg := range fd.MessageType {
		walkMsg(msg)
	}
	for _, ext := range fd.Extension {
		rewriteTypeName(ext, oldFull, newFull, oldNoDot, newNoDot)
	}
	for _, svc := range fd.Service {
		for _, m := range svc.Method {
			if t := m.GetInputType(); t == oldFull || t == oldNoDot {
				m.InputType = proto.String(pickTypeName(t, newFull, newNoDot))
			}
			if t := m.GetOutputType(); t == oldFull || t == oldNoDot {
				m.OutputType = proto.String(pickTypeName(t, newFull, newNoDot))
			}
		}
	}
}

func rewriteTypeName(field *descriptorpb.FieldDescriptorProto, oldFull, newFull, oldNoDot, newNoDot string) {
	t := field.GetTypeName()
	if t == oldFull || t == oldNoDot {
		field.TypeName = proto.String(pickTypeName(t, newFull, newNoDot))
	}
}

func pickTypeName(current, newFull, newNoDot string) string {
	if strings.HasPrefix(current, ".") {
		return newFull
	}
	return newNoDot
}

// goCamelCase mirrors google.golang.org/protobuf/internal/strs.GoCamelCase.
func goCamelCase(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	wasUnderscore := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_':
			wasUnderscore = true
		case wasUnderscore || i == 0:
			b.WriteByte(toASCIIUpper(c))
			wasUnderscore = false
		default:
			b.WriteByte(c)
		}
	}
	// Trailing underscore(s) become a trailing 'X' (protobuf GoCamelCase).
	if wasUnderscore {
		b.WriteByte('X')
	}
	return b.String()
}

func toASCIIUpper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - 'a' + 'A'
	}
	return c
}

func codegenFromDescriptors(cfg *Config, deps *Deps, fds []*descriptorpb.FileDescriptorProto) error {
	if len(fds) == 0 {
		return fmt.Errorf("no descriptors to generate")
	}
	for _, fd := range fds {
		fixNestedOneofCollisions(fd)
		injectGoPackageFD(fd)
	}
	set := &descriptorpb.FileDescriptorSet{File: fds}
	setPath := filepath.Join(cfg.CacheDir, extractSubdir, "descriptors.pb")
	if err := os.MkdirAll(filepath.Dir(setPath), 0o755); err != nil {
		return err
	}
	data, err := proto.Marshal(set)
	if err != nil {
		return err
	}
	if err := os.WriteFile(setPath, data, 0o644); err != nil {
		return err
	}

	_ = os.RemoveAll(cfg.ProtoOut)
	if err := os.MkdirAll(cfg.ProtoOut, 0o755); err != nil {
		return err
	}

	args := []string{
		"--descriptor_set_in=" + setPath,
		"--go_out=" + cfg.ProtoOut,
		"--go_opt=paths=source_relative",
	}
	for _, fd := range fds {
		args = append(args, fd.GetName())
	}
	fmt.Printf("Running protoc on %d descriptor(s)...\n", len(fds))
	cmd := exec.Command(deps.Protoc, args...)
	cmd.Env = append(os.Environ(), "PATH="+deps.PluginDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("protoc: %w", err)
	}
	return nil
}
