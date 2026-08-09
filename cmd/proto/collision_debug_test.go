package main

import (
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestDebugExaFetchCollisions(t *testing.T) {
	root := findModuleRoot()
	raw := filepath.Join(root, ".tmp/proto/extract/raw/agent.proto.fd")
	data, err := os.ReadFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	var fd descriptorpb.FileDescriptorProto
	if err := proto.Unmarshal(data, &fd); err != nil {
		t.Fatal(err)
	}
	t.Logf("file=%s pkg=%s messages=%d", fd.GetName(), fd.GetPackage(), len(fd.MessageType))
	find := func(msgs []*descriptorpb.DescriptorProto, name string) *descriptorpb.DescriptorProto {
		for _, m := range msgs {
			if m.GetName() == name {
				return m
			}
		}
		return nil
	}
	msg := find(fd.MessageType, "ExaFetchRequestResponse")
	if msg == nil {
		t.Fatal("ExaFetchRequestResponse not found at top level")
	}
	t.Logf("nested=%d fields=%d oneofs=%d", len(msg.NestedType), len(msg.Field), len(msg.OneofDecl))
	for _, n := range msg.NestedType {
		t.Logf("  nested name=%q go=%q", n.GetName(), goCamelCase(n.GetName()))
	}
	for _, f := range msg.Field {
		t.Logf("  field name=%q go=%q oneof=%v type_name=%q", f.GetName(), goCamelCase(f.GetName()), f.OneofIndex, f.GetTypeName())
	}
}
