package cursor_api_sdk

import (
	"fmt"

	cursorProto "github.com/CoreUnit-NET/cursed-gateway/lib/cursorProto"
)

const headlessInteractionReason = "This Cursor interaction requires UI approval and is not available through the gateway."

// handleInteractionQuery answers InteractionQuery on the open Run stream.
// Unanswered queries stall the same way as unanswered KV / request_context.
func handleInteractionQuery(query *cursorProto.InteractionQuery, write func(*cursorProto.AgentClientMessage) error) error {
	if query == nil {
		return nil
	}
	resp := &cursorProto.InteractionResponse{Id: query.GetId()}
	switch query.Query.(type) {
	case *cursorProto.InteractionQuery_WebSearchRequestQuery:
		resp.Result = &cursorProto.InteractionResponse_WebSearchRequestResponse{
			WebSearchRequestResponse: &cursorProto.WebSearchRequestResponse{
				Result: &cursorProto.WebSearchRequestResponse_Rejected{
					Rejected: &cursorProto.WebSearchRequestResponse_RejectedMsg{Reason: headlessInteractionReason},
				},
			},
		}
	case *cursorProto.InteractionQuery_AskQuestionInteractionQuery:
		resp.Result = &cursorProto.InteractionResponse_AskQuestionInteractionResponse{
			AskQuestionInteractionResponse: &cursorProto.AskQuestionInteractionResponse{
				Result: &cursorProto.AskQuestionResult{
					Result: &cursorProto.AskQuestionResult_Rejected{
						Rejected: &cursorProto.AskQuestionRejected{Reason: headlessInteractionReason},
					},
				},
			},
		}
	case *cursorProto.InteractionQuery_SwitchModeRequestQuery:
		resp.Result = &cursorProto.InteractionResponse_SwitchModeRequestResponse{
			SwitchModeRequestResponse: &cursorProto.SwitchModeRequestResponse{
				Result: &cursorProto.SwitchModeRequestResponse_Rejected{
					Rejected: &cursorProto.SwitchModeRequestResponse_RejectedMsg{Reason: headlessInteractionReason},
				},
			},
		}
	case *cursorProto.InteractionQuery_ExaSearchRequestQuery:
		resp.Result = &cursorProto.InteractionResponse_ExaSearchRequestResponse{
			ExaSearchRequestResponse: &cursorProto.ExaSearchRequestResponse{
				Result: &cursorProto.ExaSearchRequestResponse_Rejected{
					Rejected: &cursorProto.ExaSearchRequestResponse_RejectedMsg{Reason: headlessInteractionReason},
				},
			},
		}
	case *cursorProto.InteractionQuery_ExaFetchRequestQuery:
		resp.Result = &cursorProto.InteractionResponse_ExaFetchRequestResponse{
			ExaFetchRequestResponse: &cursorProto.ExaFetchRequestResponse{
				Result: &cursorProto.ExaFetchRequestResponse_Rejected{
					Rejected: &cursorProto.ExaFetchRequestResponse_RejectedMsg{Reason: headlessInteractionReason},
				},
			},
		}
	case *cursorProto.InteractionQuery_CreatePlanRequestQuery:
		// Headless CLI parity: auto-ack plan creation with empty plan_uri.
		resp.Result = &cursorProto.InteractionResponse_CreatePlanRequestResponse{
			CreatePlanRequestResponse: &cursorProto.CreatePlanRequestResponse{
				Result: &cursorProto.CreatePlanResult{
					PlanUri: "",
					Result: &cursorProto.CreatePlanResult_Success{
						Success: &cursorProto.CreatePlanSuccess{},
					},
				},
			},
		}
	case *cursorProto.InteractionQuery_SetupVmEnvironmentArgs:
		resp.Result = &cursorProto.InteractionResponse_SetupVmEnvironmentResult{
			SetupVmEnvironmentResult: &cursorProto.SetupVmEnvironmentResult{
				Result: &cursorProto.SetupVmEnvironmentResult_Success{
					Success: &cursorProto.SetupVmEnvironmentSuccess{},
				},
			},
		}
	default:
		return fmt.Errorf("unsupported interaction query variant")
	}
	return write(&cursorProto.AgentClientMessage{
		Message: &cursorProto.AgentClientMessage_InteractionResponse{
			InteractionResponse: resp,
		},
	})
}
