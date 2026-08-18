package cursor_api_sdk

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	cursorProto "github.com/CoreUnit-NET/cursed-gateway/lib/cursorProto"
	"google.golang.org/protobuf/proto"
)

// ChatMessage is a minimal OpenAI chat message (tools-aware).
type ChatMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []OpenAIToolCall `json:"tool_calls,omitempty"`
	// Images are decoded from multipart content (data/base64 only); not serialized.
	Images []Image `json:"-"`
}

// UnmarshalJSON accepts OpenAI content as a string, null, or array of parts
// (OpenCode / Chat Completions multipart): flattens text and extracts images.
func (m *ChatMessage) UnmarshalJSON(data []byte) error {
	type alias ChatMessage
	aux := &struct {
		Content json.RawMessage `json:"content"`
		*alias
	}{alias: (*alias)(m)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	text, err := flattenMessageContent(aux.Content)
	if err != nil {
		return err
	}
	m.Content = text
	m.Images = extractImagesFromContent(aux.Content)
	return nil
}

func flattenMessageContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(raw, &parts); err == nil {
		return joinContentParts(parts)
	}
	if text, ok, err := contentPartText(raw); err != nil {
		return "", err
	} else if ok {
		return text, nil
	}
	return "", fmt.Errorf("content must be a string or array of parts")
}

func joinContentParts(parts []json.RawMessage) (string, error) {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		var s string
		if err := json.Unmarshal(part, &s); err == nil {
			if s != "" {
				texts = append(texts, s)
			}
			continue
		}
		text, ok, err := contentPartText(part)
		if err != nil {
			return "", err
		}
		if ok && text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n"), nil
}

func contentPartText(raw json.RawMessage) (string, bool, error) {
	var part struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &part); err != nil {
		return "", false, fmt.Errorf("invalid content part")
	}
	switch strings.ToLower(strings.TrimSpace(part.Type)) {
	case "", "text", "input_text", "output_text":
		return part.Text, true, nil
	default:
		return "", false, nil
	}
}

// ConversationTurn is a prior user/assistant pair.
type ConversationTurn struct {
	UserText      string
	AssistantText string
}

// ParsedChat is the OpenAI → Cursor mapping of a chat request.
type ParsedChat struct {
	SystemPrompt string
	Turns        []ConversationTurn
	UserText     string
	// UserImages are action-turn attachments only (history stays text-only).
	UserImages  []Image
	ToolResults []ToolResultInfo
	// StickyConversationID, when set, is used as AgentRunRequest.conversation_id
	// instead of a random UUID (P2.1 identity keying).
	StickyConversationID string
}

// ParseChatMessages splits OpenAI messages into system / history / current user / tool results.
// Image parts on the action user are kept; prior user images are dropped (history text-only).
func ParseChatMessages(messages []ChatMessage) ParsedChat {
	var systems []string
	var turns []ConversationTurn
	var pendingUser string
	var pendingImages []Image
	var toolResults []ToolResultInfo
	var havePendingUser bool

	for _, m := range messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		text := strings.TrimSpace(m.Content)
		switch role {
		case "system":
			if text != "" {
				systems = append(systems, text)
			}
		case "tool":
			toolResults = append(toolResults, ToolResultInfo{
				ToolCallID: strings.TrimSpace(m.ToolCallID),
				Content:    m.Content,
			})
		case "user":
			// History turns are text-only; prior images are not carried forward.
			if havePendingUser && pendingUser != "" {
				turns = append(turns, ConversationTurn{UserText: pendingUser})
			}
			pendingUser = text
			pendingImages = append([]Image(nil), m.Images...)
			havePendingUser = true
		case "assistant":
			// Skip tool_calls-only assistants with no prior user text (continuation is via mcpResult).
			if havePendingUser && pendingUser != "" {
				turns = append(turns, ConversationTurn{UserText: pendingUser, AssistantText: text})
			}
			pendingUser = ""
			pendingImages = nil
			havePendingUser = false
		}
	}

	system := strings.Join(systems, "\n")
	if system == "" {
		system = "You are a helpful assistant."
	}

	userText := pendingUser
	userImages := pendingImages
	if !havePendingUser && userText == "" && len(userImages) == 0 && len(toolResults) == 0 && len(turns) > 0 {
		last := turns[len(turns)-1]
		turns = turns[:len(turns)-1]
		userText = last.UserText
	}

	return ParsedChat{
		SystemPrompt: system,
		Turns:        turns,
		UserText:     userText,
		UserImages:   userImages,
		ToolResults:  toolResults,
	}
}

// RunPayload is a framed-ready AgentClientMessage plus local blob store for KV.
type RunPayload struct {
	RequestBytes []byte
	BlobStore    map[string][]byte // hex(blobID) → bytes
	Conversation string
	ModelID      string
	// Tools is echoed into exec request_context replies (may be empty).
	Tools []*cursorProto.McpToolDefinition
	// CheckpointMode is miss|hit|rebuild for sticky P2.2 logging.
	CheckpointMode string
	// CheckpointKey, when set, captures conversation_checkpoint_update into the store.
	CheckpointKey string
	// OnCheckpoint is invoked on each ConversationCheckpointUpdate (may be nil).
	OnCheckpoint func(state *cursorProto.ConversationStateStructure, blobs map[string][]byte)
}

// BuildRunPayload builds an AgentClientMessage run_request (blob system prompt strategy).
func BuildRunPayload(modelID string, parsed ParsedChat) (*RunPayload, error) {
	return BuildRunPayloadSelection(LiteralModelSelection(modelID), parsed)
}

// BuildRunPayloadWithCheckpoint builds a run request, optionally merging a prior sticky checkpoint.
func BuildRunPayloadWithCheckpoint(sel ModelSelection, parsed ParsedChat, prior *StoredCheckpoint) (*RunPayload, error) {
	return buildRunPayloadSelection(sel, parsed, prior)
}

// rootRoleMessage / rootTextPart encode rootPromptMessagesJson like oauth
// (JSON.stringify insertion order), not Go map key sort.
type rootRoleMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type rootTextPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// storeBlob content-addresses raw bytes: 32-byte sha256 id on the wire,
// full payload in BlobStore under hex(id). Cursor Structure `bytes` fields
// expect ids, not inlined protobuf (see oauth proxy storeBlob).
func storeBlob(store map[string][]byte, raw []byte) []byte {
	sum := sha256.Sum256(raw)
	id := append([]byte(nil), sum[:]...)
	store[hex.EncodeToString(id)] = append([]byte(nil), raw...)
	return id
}

func storeJSONBlob(store map[string][]byte, v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return storeBlob(store, raw), nil
}

// BuildRunPayloadSelection builds a run request using catalog-resolved model identity.
// ModelDetails.model_id and RequestedModel.model_id use the agent wire id (legacy slug
// when present). OpenAI response model id stays the public/catalog id.
//
// Every ConversationState Structure `bytes` field is a 32-byte sha256 blob id;
// raw bytes live only in RunPayload.BlobStore (served by handleKV getBlob).
func BuildRunPayloadSelection(sel ModelSelection, parsed ParsedChat) (*RunPayload, error) {
	return buildRunPayloadSelection(sel, parsed, nil)
}

func buildRunPayloadSelection(sel ModelSelection, parsed ParsedChat, prior *StoredCheckpoint) (*RunPayload, error) {
	if strings.TrimSpace(parsed.UserText) == "" && len(parsed.UserImages) == 0 {
		return nil, fmt.Errorf("chat request missing user message")
	}
	if sel.PublicID == "" {
		sel = LiteralModelSelection(sel.WireModelID)
	}
	if sel.WireModelID == "" {
		sel.WireModelID = sel.PublicID
	}
	if sel.DisplayName == "" {
		sel.DisplayName = sel.PublicID
	}

	blobStore := map[string][]byte{}
	mode := CheckpointMiss
	if prior != nil && prior.State != nil {
		SeedBlobStore(blobStore, prior.Blobs)
	}

	// Cursor builds the model prompt from rootPromptMessagesJson, not turns[] alone.
	// JSON field order must match oauth/JS (role before content; type before text)
	// so content-addressed blob IDs stay compatible with server-echoed checkpoints.
	root := make([][]byte, 0, 1+2*len(parsed.Turns))
	sysID, err := storeJSONBlob(blobStore, rootRoleMessage{Role: "system", Content: parsed.SystemPrompt})
	if err != nil {
		return nil, err
	}
	root = append(root, sysID)
	for _, turn := range parsed.Turns {
		userRootID, err := storeJSONBlob(blobStore, rootRoleMessage{
			Role:    "user",
			Content: []rootTextPart{{Type: "text", Text: turn.UserText}},
		})
		if err != nil {
			return nil, err
		}
		root = append(root, userRootID)
		if turn.AssistantText != "" {
			asstRootID, err := storeJSONBlob(blobStore, rootRoleMessage{
				Role:    "assistant",
				Content: []rootTextPart{{Type: "text", Text: turn.AssistantText}},
			})
			if err != nil {
				return nil, err
			}
			root = append(root, asstRootID)
		}
	}

	// turns[]: blobify nested userMsg / steps, then the turn envelope.
	turnIDs := make([][]byte, 0, len(parsed.Turns))
	for _, turn := range parsed.Turns {
		userMsgBytes, err := proto.Marshal(&cursorProto.UserMessage{
			Text:      turn.UserText,
			MessageId: newUUID(),
		})
		if err != nil {
			return nil, err
		}
		userMsgID := storeBlob(blobStore, userMsgBytes)

		var stepIDs [][]byte
		if turn.AssistantText != "" {
			stepBytes, err := proto.Marshal(&cursorProto.ConversationStep{
				Message: &cursorProto.ConversationStep_AssistantMessage{
					AssistantMessage: &cursorProto.AssistantMessage{Text: turn.AssistantText},
				},
			})
			if err != nil {
				return nil, err
			}
			stepIDs = append(stepIDs, storeBlob(blobStore, stepBytes))
		}

		turnBytes, err := proto.Marshal(&cursorProto.ConversationTurnStructure{
			Turn: &cursorProto.ConversationTurnStructure_AgentConversationTurn{
				AgentConversationTurn: &cursorProto.AgentConversationTurnStructure{
					UserMessage: userMsgID,
					Steps:       stepIDs,
				},
			},
		})
		if err != nil {
			return nil, err
		}
		turnIDs = append(turnIDs, storeBlob(blobStore, turnBytes))
	}

	convID := strings.TrimSpace(parsed.StickyConversationID)
	if convID == "" {
		convID = newUUID()
	}

	var state *cursorProto.ConversationStateStructure
	if prior != nil && prior.State != nil && SystemPromptCompatible(prior.State, [][]byte{sysID}, blobStore, parsed.SystemPrompt) {
		state = MergeCheckpointState(prior.State, root, turnIDs)
		mode = CheckpointHit
	} else {
		if prior != nil && prior.State != nil {
			mode = CheckpointRebuild
		}
		state = &cursorProto.ConversationStateStructure{
			RootPromptMessagesJson: root,
			Turns:                  turnIDs,
			Todos:                  nil,
			PendingToolCalls:       nil,
			PreviousWorkspaceUris:  nil,
			FileStates:             map[string][]byte{},
			FileStatesV2:           map[string]*cursorProto.FileStateStructure{},
			SummaryArchives:        nil,
			TurnTimings:            nil,
			SubagentStates:         map[string]*cursorProto.SubagentPersistedState{},
			SelfSummaryCount:       0,
			ReadPaths:              nil,
		}
	}

	maxMode := sel.MaxMode
	params := make([]*cursorProto.RequestedModel_ModelParameterbytes, 0, len(sel.Parameters))
	for _, p := range sel.Parameters {
		params = append(params, &cursorProto.RequestedModel_ModelParameterbytes{
			Id:    p.ID,
			Value: p.Value,
		})
	}

	actionUser := &cursorProto.UserMessage{
		Text:      parsed.UserText,
		MessageId: newUUID(),
	}
	if imgs := selectedImagesFromParsed(blobStore, parsed.UserImages); len(imgs) > 0 {
		actionUser.SelectedContext = &cursorProto.SelectedContext{SelectedImages: imgs}
	}

	runReq := &cursorProto.AgentRunRequest{
		ConversationState: state,
		Action: &cursorProto.ConversationAction{
			Action: &cursorProto.ConversationAction_UserMessageAction{
				UserMessageAction: &cursorProto.UserMessageAction{
					UserMessage: actionUser,
				},
			},
		},
		ModelDetails: &cursorProto.ModelDetails{
			ModelId:        sel.WireModelID,
			DisplayModelId: sel.PublicID,
			DisplayName:    sel.DisplayName,
			MaxMode:        &maxMode,
		},
		RequestedModel: &cursorProto.RequestedModel{
			ModelId:    sel.WireModelID,
			MaxMode:    sel.MaxMode,
			Parameters: params,
		},
		ConversationId: proto.String(convID),
	}

	clientMsg := &cursorProto.AgentClientMessage{
		Message: &cursorProto.AgentClientMessage_RunRequest{RunRequest: runReq},
	}
	reqBytes, err := proto.Marshal(clientMsg)
	if err != nil {
		return nil, err
	}
	return &RunPayload{
		RequestBytes:   reqBytes,
		BlobStore:      blobStore,
		Conversation:   convID,
		ModelID:        sel.PublicID,
		CheckpointMode: mode,
	}, nil
}

// selectedImagesFromParsed blobifies action-user images and builds SelectedImage
// messages with BlobIdWithData (otto proxy.ts 1316–1344).
func selectedImagesFromParsed(store map[string][]byte, images []Image) []*cursorProto.SelectedImage {
	if len(images) == 0 {
		return nil
	}
	out := make([]*cursorProto.SelectedImage, 0, len(images))
	for _, img := range images {
		if len(img.Bytes) == 0 {
			continue
		}
		id := storeBlob(store, img.Bytes)
		mime := strings.TrimSpace(img.MimeType)
		if mime == "" {
			mime = "image/png"
		}
		path := strings.TrimSpace(img.Filename)
		if path == "" {
			path = "attachment"
		}
		out = append(out, &cursorProto.SelectedImage{
			Uuid:     newUUID(),
			Path:     path,
			MimeType: mime,
			DataOrBlobId: &cursorProto.SelectedImage_BlobIdWithData{
				BlobIdWithData: &cursorProto.SelectedImage_BlobIdWithDataMsg{
					BlobId: id,
					Data:   append([]byte(nil), img.Bytes...),
				},
			},
		})
	}
	return out
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
