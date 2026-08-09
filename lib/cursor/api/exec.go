package cursor_api_sdk

import (
	"fmt"

	cursorProto "github.com/CoreUnit-NET/cursed-gateway/lib/cursorProto"
)

const nativeToolRejectReason = "Tool not available in this environment. Use the MCP tools provided instead."

// handleExec replies to exec_server_message so AgentService/Run does not stall.
// request_context returns the advertised MCP tool list (may be empty).
// Native Cursor tools are rejected; MCP tool calls without a bridge get an error.
func handleExec(msg *cursorProto.ExecServerMessage, tools []*cursorProto.McpToolDefinition, write func(*cursorProto.AgentClientMessage) error) error {
	if msg == nil {
		return nil
	}
	switch m := msg.Message.(type) {
	case *cursorProto.ExecServerMessage_RequestContextArgs:
		return writeExecClient(msg, &cursorProto.ExecClientMessage{
			Id:     msg.GetId(),
			ExecId: msg.GetExecId(),
			Message: &cursorProto.ExecClientMessage_RequestContextResult{
				RequestContextResult: &cursorProto.RequestContextResult{
					Result: &cursorProto.RequestContextResult_Success{
						Success: &cursorProto.RequestContextSuccess{
							RequestContext: &cursorProto.RequestContext{
								Tools:        tools,
								FileContents: map[string]string{},
							},
						},
					},
				},
			},
		}, write)

	case *cursorProto.ExecServerMessage_McpArgs:
		name := ""
		if m.McpArgs != nil {
			name = m.McpArgs.GetToolName()
			if name == "" {
				name = m.McpArgs.GetName()
			}
		}
		return writeExecClient(msg, &cursorProto.ExecClientMessage{
			Id:     msg.GetId(),
			ExecId: msg.GetExecId(),
			Message: &cursorProto.ExecClientMessage_McpResult{
				McpResult: &cursorProto.McpResult{
					Result: &cursorProto.McpResult_Error{
						Error: &cursorProto.McpError{
							Error: fmt.Sprintf("MCP tool %q is not available in this gateway yet", name),
						},
					},
				},
			},
		}, write)

	case *cursorProto.ExecServerMessage_ReadArgs:
		path := ""
		if m.ReadArgs != nil {
			path = m.ReadArgs.GetPath()
		}
		return writeExecClient(msg, &cursorProto.ExecClientMessage{
			Id:     msg.GetId(),
			ExecId: msg.GetExecId(),
			Message: &cursorProto.ExecClientMessage_ReadResult{
				ReadResult: &cursorProto.ReadResult{
					Result: &cursorProto.ReadResult_Rejected{
						Rejected: &cursorProto.ReadRejected{Path: path, Reason: nativeToolRejectReason},
					},
				},
			},
		}, write)

	case *cursorProto.ExecServerMessage_LsArgs:
		path := ""
		if m.LsArgs != nil {
			path = m.LsArgs.GetPath()
		}
		return writeExecClient(msg, &cursorProto.ExecClientMessage{
			Id:     msg.GetId(),
			ExecId: msg.GetExecId(),
			Message: &cursorProto.ExecClientMessage_LsResult{
				LsResult: &cursorProto.LsResult{
					Result: &cursorProto.LsResult_Rejected{
						Rejected: &cursorProto.LsRejected{Path: path, Reason: nativeToolRejectReason},
					},
				},
			},
		}, write)

	case *cursorProto.ExecServerMessage_GrepArgs:
		return writeExecClient(msg, &cursorProto.ExecClientMessage{
			Id:     msg.GetId(),
			ExecId: msg.GetExecId(),
			Message: &cursorProto.ExecClientMessage_GrepResult{
				GrepResult: &cursorProto.GrepResult{
					Result: &cursorProto.GrepResult_Error{
						Error: &cursorProto.GrepError{Error: nativeToolRejectReason},
					},
				},
			},
		}, write)

	case *cursorProto.ExecServerMessage_WriteArgs:
		path := ""
		if m.WriteArgs != nil {
			path = m.WriteArgs.GetPath()
		}
		return writeExecClient(msg, &cursorProto.ExecClientMessage{
			Id:     msg.GetId(),
			ExecId: msg.GetExecId(),
			Message: &cursorProto.ExecClientMessage_WriteResult{
				WriteResult: &cursorProto.WriteResult{
					Result: &cursorProto.WriteResult_Rejected{
						Rejected: &cursorProto.WriteRejected{Path: path, Reason: nativeToolRejectReason},
					},
				},
			},
		}, write)

	case *cursorProto.ExecServerMessage_DeleteArgs:
		path := ""
		if m.DeleteArgs != nil {
			path = m.DeleteArgs.GetPath()
		}
		return writeExecClient(msg, &cursorProto.ExecClientMessage{
			Id:     msg.GetId(),
			ExecId: msg.GetExecId(),
			Message: &cursorProto.ExecClientMessage_DeleteResult{
				DeleteResult: &cursorProto.DeleteResult{
					Result: &cursorProto.DeleteResult_Rejected{
						Rejected: &cursorProto.DeleteRejected{Path: path, Reason: nativeToolRejectReason},
					},
				},
			},
		}, write)

	case *cursorProto.ExecServerMessage_ShellArgs:
		return writeExecClient(msg, &cursorProto.ExecClientMessage{
			Id:     msg.GetId(),
			ExecId: msg.GetExecId(),
			Message: &cursorProto.ExecClientMessage_ShellResult{
				ShellResult: rejectShellResult(m.ShellArgs),
			},
		}, write)

	case *cursorProto.ExecServerMessage_ShellStreamArgs:
		return writeExecClient(msg, &cursorProto.ExecClientMessage{
			Id:     msg.GetId(),
			ExecId: msg.GetExecId(),
			Message: &cursorProto.ExecClientMessage_ShellStream{
				ShellStream: &cursorProto.ShellStream{
					Event: &cursorProto.ShellStream_Rejected{
						Rejected: rejectShell(m.ShellStreamArgs),
					},
				},
			},
		}, write)

	case *cursorProto.ExecServerMessage_BackgroundShellSpawnArgs:
		args := m.BackgroundShellSpawnArgs
		cmd, cwd := "", ""
		if args != nil {
			cmd, cwd = args.GetCommand(), args.GetWorkingDirectory()
		}
		return writeExecClient(msg, &cursorProto.ExecClientMessage{
			Id:     msg.GetId(),
			ExecId: msg.GetExecId(),
			Message: &cursorProto.ExecClientMessage_BackgroundShellSpawnResult{
				BackgroundShellSpawnResult: &cursorProto.BackgroundShellSpawnResult{
					Result: &cursorProto.BackgroundShellSpawnResult_Rejected{
						Rejected: &cursorProto.ShellRejected{
							Command:          cmd,
							WorkingDirectory: cwd,
							Reason:           nativeToolRejectReason,
							IsReadonly:       false,
						},
					},
				},
			},
		}, write)

	case *cursorProto.ExecServerMessage_WriteShellStdinArgs:
		return writeExecClient(msg, &cursorProto.ExecClientMessage{
			Id:     msg.GetId(),
			ExecId: msg.GetExecId(),
			Message: &cursorProto.ExecClientMessage_WriteShellStdinResult{
				WriteShellStdinResult: &cursorProto.WriteShellStdinResult{
					Result: &cursorProto.WriteShellStdinResult_Error{
						Error: &cursorProto.WriteShellStdinError{Error: nativeToolRejectReason},
					},
				},
			},
		}, write)

	case *cursorProto.ExecServerMessage_FetchArgs:
		url := ""
		if m.FetchArgs != nil {
			url = m.FetchArgs.GetUrl()
		}
		return writeExecClient(msg, &cursorProto.ExecClientMessage{
			Id:     msg.GetId(),
			ExecId: msg.GetExecId(),
			Message: &cursorProto.ExecClientMessage_FetchResult{
				FetchResult: &cursorProto.FetchResult{
					Result: &cursorProto.FetchResult_Error{
						Error: &cursorProto.FetchError{Url: url, Error: nativeToolRejectReason},
					},
				},
			},
		}, write)

	case *cursorProto.ExecServerMessage_DiagnosticsArgs:
		path := ""
		if m.DiagnosticsArgs != nil {
			path = m.DiagnosticsArgs.GetPath()
		}
		return writeExecClient(msg, &cursorProto.ExecClientMessage{
			Id:     msg.GetId(),
			ExecId: msg.GetExecId(),
			Message: &cursorProto.ExecClientMessage_DiagnosticsResult{
				DiagnosticsResult: &cursorProto.DiagnosticsResult{
					Result: &cursorProto.DiagnosticsResult_Success{
						Success: &cursorProto.DiagnosticsSuccess{
							Path:             path,
							Diagnostics:      nil,
							TotalDiagnostics: 0,
						},
					},
				},
			},
		}, write)

	case *cursorProto.ExecServerMessage_ListMcpResourcesExecArgs:
		return writeExecClient(msg, &cursorProto.ExecClientMessage{
			Id:     msg.GetId(),
			ExecId: msg.GetExecId(),
			Message: &cursorProto.ExecClientMessage_ListMcpResourcesExecResult{
				ListMcpResourcesExecResult: &cursorProto.ListMcpResourcesExecResult{
					Result: &cursorProto.ListMcpResourcesExecResult_Error{
						Error: &cursorProto.ListMcpResourcesError{Error: nativeToolRejectReason},
					},
				},
			},
		}, write)

	case *cursorProto.ExecServerMessage_ReadMcpResourceExecArgs:
		uri := ""
		if m.ReadMcpResourceExecArgs != nil {
			uri = m.ReadMcpResourceExecArgs.GetUri()
		}
		return writeExecClient(msg, &cursorProto.ExecClientMessage{
			Id:     msg.GetId(),
			ExecId: msg.GetExecId(),
			Message: &cursorProto.ExecClientMessage_ReadMcpResourceExecResult{
				ReadMcpResourceExecResult: &cursorProto.ReadMcpResourceExecResult{
					Result: &cursorProto.ReadMcpResourceExecResult_Error{
						Error: &cursorProto.ReadMcpResourceError{Uri: uri, Error: nativeToolRejectReason},
					},
				},
			},
		}, write)

	case *cursorProto.ExecServerMessage_RecordScreenArgs:
		return writeExecClient(msg, &cursorProto.ExecClientMessage{
			Id:     msg.GetId(),
			ExecId: msg.GetExecId(),
			Message: &cursorProto.ExecClientMessage_RecordScreenResult{
				RecordScreenResult: &cursorProto.RecordScreenResult{
					Result: &cursorProto.RecordScreenResult_Failure{
						Failure: &cursorProto.RecordScreenFailure{Error: nativeToolRejectReason},
					},
				},
			},
		}, write)

	case *cursorProto.ExecServerMessage_ComputerUseArgs:
		return writeExecClient(msg, &cursorProto.ExecClientMessage{
			Id:     msg.GetId(),
			ExecId: msg.GetExecId(),
			Message: &cursorProto.ExecClientMessage_ComputerUseResult{
				ComputerUseResult: &cursorProto.ComputerUseResult{
					Result: &cursorProto.ComputerUseResult_Error{
						Error: &cursorProto.ComputerUseError{Error: nativeToolRejectReason},
					},
				},
			},
		}, write)
	}
	// Unknown exec variant: ignore rather than abort the run.
	return nil
}

func rejectShell(args *cursorProto.ShellArgs) *cursorProto.ShellRejected {
	cmd, cwd := "", ""
	if args != nil {
		cmd, cwd = args.GetCommand(), args.GetWorkingDirectory()
	}
	return &cursorProto.ShellRejected{
		Command:          cmd,
		WorkingDirectory: cwd,
		Reason:           nativeToolRejectReason,
		IsReadonly:       false,
	}
}

func rejectShellResult(args *cursorProto.ShellArgs) *cursorProto.ShellResult {
	return &cursorProto.ShellResult{
		Result: &cursorProto.ShellResult_Rejected{Rejected: rejectShell(args)},
	}
}

func writeExecClient(_ *cursorProto.ExecServerMessage, client *cursorProto.ExecClientMessage, write func(*cursorProto.AgentClientMessage) error) error {
	return write(&cursorProto.AgentClientMessage{
		Message: &cursorProto.AgentClientMessage_ExecClientMessage{
			ExecClientMessage: client,
		},
	})
}
