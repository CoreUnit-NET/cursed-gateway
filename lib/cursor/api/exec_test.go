package cursor_api_sdk

import (
	"testing"

	cursorProto "github.com/CoreUnit-NET/cursed-gateway/lib/cursorProto"
)

func TestHandleExecRequestContext(t *testing.T) {
	tools := []*cursorProto.McpToolDefinition{{Name: "demo"}}
	var got *cursorProto.AgentClientMessage
	err := handleExec(&cursorProto.ExecServerMessage{
		Id:     7,
		ExecId: "exec-1",
		Message: &cursorProto.ExecServerMessage_RequestContextArgs{
			RequestContextArgs: &cursorProto.RequestContextArgs{},
		},
	}, tools, func(msg *cursorProto.AgentClientMessage) error {
		got = msg
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	exec := got.GetExecClientMessage()
	if exec == nil || exec.GetId() != 7 || exec.GetExecId() != "exec-1" {
		t.Fatalf("exec client = %#v", exec)
	}
	rc := exec.GetRequestContextResult().GetSuccess().GetRequestContext()
	if rc == nil || len(rc.GetTools()) != 1 || rc.GetTools()[0].GetName() != "demo" {
		t.Fatalf("request context = %#v", rc)
	}
}

func TestHandleExecRejectsNativeRead(t *testing.T) {
	var got *cursorProto.AgentClientMessage
	err := handleExec(&cursorProto.ExecServerMessage{
		Id:     3,
		ExecId: "exec-read",
		Message: &cursorProto.ExecServerMessage_ReadArgs{
			ReadArgs: &cursorProto.ReadArgs{Path: "/tmp/x"},
		},
	}, nil, func(msg *cursorProto.AgentClientMessage) error {
		got = msg
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	rejected := got.GetExecClientMessage().GetReadResult().GetRejected()
	if rejected == nil || rejected.GetPath() != "/tmp/x" || rejected.GetReason() == "" {
		t.Fatalf("rejected = %#v", rejected)
	}
}

func TestHandleInteractionQueryCreatePlan(t *testing.T) {
	var got *cursorProto.AgentClientMessage
	err := handleInteractionQuery(&cursorProto.InteractionQuery{
		Id: 9,
		Query: &cursorProto.InteractionQuery_CreatePlanRequestQuery{
			CreatePlanRequestQuery: &cursorProto.CreatePlanRequestQuery{},
		},
	}, func(msg *cursorProto.AgentClientMessage) error {
		got = msg
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := got.GetInteractionResponse()
	if resp == nil || resp.GetId() != 9 {
		t.Fatalf("response = %#v", resp)
	}
	if resp.GetCreatePlanRequestResponse().GetResult().GetSuccess() == nil {
		t.Fatalf("expected create_plan success, got %#v", resp.GetCreatePlanRequestResponse())
	}
}
