package cursor_api_sdk

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	cursorProto "github.com/CoreUnit-NET/cursed-gateway/lib/cursorProto"
	"google.golang.org/protobuf/proto"
)

// StreamEvent is a high-level event from AgentService/Run.
type StreamEvent struct {
	Text       string
	Thinking   bool
	TurnEnded  bool
	ToolCall   *PendingExec
	Err        error
	HTTPStatus int
}

// RunControl owns a live AgentService/Run stream that can pause for tool results.
type RunControl struct {
	Events <-chan StreamEvent

	writeMu    sync.Mutex
	writeFrame func(*cursorProto.AgentClientMessage) error
	cancel     context.CancelFunc
	pw         *io.PipeWriter
	pending    []PendingExec
	preface    []StreamEvent // events pushed back by tool-call drains
	usage      usageAcc
	mu         sync.Mutex
}

// Usage returns the Path A token meter accumulated on this run so far.
func (r *RunControl) Usage() Usage {
	if r == nil {
		return Usage{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.usage.snapshot()
}

func (r *RunControl) notePromptTokens(used int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.usage.notePrompt(used)
}

func (r *RunControl) noteCompletionTokens(delta int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.usage.noteCompletion(delta)
}

// Recv returns the next stream event, preferring any unread preface events.
func (r *RunControl) Recv() (StreamEvent, bool) {
	if r == nil {
		return StreamEvent{}, false
	}
	r.mu.Lock()
	if len(r.preface) > 0 {
		ev := r.preface[0]
		r.preface = r.preface[1:]
		r.mu.Unlock()
		return ev, true
	}
	r.mu.Unlock()
	ev, ok := <-r.Events
	return ev, ok
}

// TryRecv returns a buffered/preface event without blocking.
func (r *RunControl) TryRecv() (StreamEvent, bool) {
	if r == nil {
		return StreamEvent{}, false
	}
	r.mu.Lock()
	if len(r.preface) > 0 {
		ev := r.preface[0]
		r.preface = r.preface[1:]
		r.mu.Unlock()
		return ev, true
	}
	r.mu.Unlock()
	select {
	case ev, ok := <-r.Events:
		if !ok {
			return StreamEvent{}, false
		}
		return ev, true
	default:
		return StreamEvent{}, false
	}
}

// Unread pushes an event so the next Recv observes it first (FIFO among unread).
func (r *RunControl) Unread(ev StreamEvent) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.preface = append(r.preface, ev)
}

// Pending returns a copy of mcpArgs waiting for OpenAI tool results.
func (r *RunControl) Pending() []PendingExec {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]PendingExec, len(r.pending))
	copy(out, r.pending)
	return out
}

// SubmitMcpResults writes mcpResult frames for parked pending execs and clears them.
func (r *RunControl) SubmitMcpResults(results []ToolResultInfo) error {
	if r == nil {
		return fmt.Errorf("run control is nil")
	}
	r.mu.Lock()
	pending := append([]PendingExec(nil), r.pending...)
	r.pending = nil
	r.mu.Unlock()

	byID := map[string]string{}
	for _, res := range results {
		byID[res.ToolCallID] = res.Content
	}
	for _, pe := range pending {
		content, ok := byID[pe.ToolCallID]
		var mcp *cursorProto.McpResult
		if ok {
			mcp = EncodeMcpSuccess(content)
		} else {
			mcp = EncodeMcpError("Tool result not provided")
		}
		msg := &cursorProto.AgentClientMessage{
			Message: &cursorProto.AgentClientMessage_ExecClientMessage{
				ExecClientMessage: &cursorProto.ExecClientMessage{
					Id:     pe.ExecMsgID,
					ExecId: pe.ExecID,
					Message: &cursorProto.ExecClientMessage_McpResult{
						McpResult: mcp,
					},
				},
			},
		}
		if err := r.writeFrame(msg); err != nil {
			return err
		}
	}
	return nil
}

// Close cancels the run and closes the request pipe.
func (r *RunControl) Close() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
	}
	if r.pw != nil {
		_ = r.pw.Close()
	}
}

func (r *RunControl) trackPending(pe PendingExec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending = append(r.pending, pe)
}

// RunChat opens AgentService/Run, handles KV/heartbeats, and emits text events.
// MCP tool calls without a bridge callback get an immediate error reply.
// The returned channel is closed when the run finishes.
func (c *Client) RunChat(ctx context.Context, accessToken string, payload *RunPayload) (<-chan StreamEvent, error) {
	rc, err := c.StartRun(ctx, accessToken, payload, false)
	if err != nil {
		return nil, err
	}
	return rc.Events, nil
}

// StartRun opens AgentService/Run. When bridgeTools is true, mcpArgs emit ToolCall
// events and the HTTP layer must park the RunControl and later SubmitMcpResults.
func (c *Client) StartRun(ctx context.Context, accessToken string, payload *RunPayload, bridgeTools bool) (*RunControl, error) {
	if payload == nil || len(payload.RequestBytes) == 0 {
		return nil, fmt.Errorf("run payload is empty")
	}
	origin, err := c.ResolveAgentOrigin(ctx, accessToken)
	if err != nil {
		origin = c.baseURL()
	}
	slog.Debug("starting agent run",
		"model", payload.ModelID,
		"origin", origin,
		"conversation", payload.Conversation,
		"bridge_tools", bridgeTools,
		"request_bytes", len(payload.RequestBytes),
	)

	// Detach from the inbound HTTP request cancel so park/resume across tool
	// turns keeps the Connect stream alive after the OpenAI response ends.
	parent := ctx
	if bridgeTools {
		parent = context.WithoutCancel(ctx)
	}
	runCtx, cancel := context.WithCancel(parent)
	pr, pw := io.Pipe()
	req, err := http.NewRequestWithContext(runCtx, http.MethodPost, origin+pathAgentRun, pr)
	if err != nil {
		cancel()
		_ = pw.Close()
		return nil, err
	}
	c.setCommonHeaders(req.Header, accessToken)
	req.Header.Set("content-type", "application/connect+proto")
	req.Header.Set("x-cursor-streaming", "true")
	req.Header.Set("connect-accept-encoding", "identity")
	req.Header.Set("user-agent", "connect-go/1.0")
	req.Header.Set("te", "trailers")

	type doResult struct {
		res *http.Response
		err error
	}
	done := make(chan doResult, 1)
	go func() {
		res, err := c.httpClient().Do(req)
		done <- doResult{res: res, err: err}
	}()

	// Write initial run frame immediately so the request is not empty.
	if _, err := pw.Write(FrameConnect(payload.RequestBytes, flagRaw)); err != nil {
		cancel()
		_ = pw.CloseWithError(err)
		return nil, err
	}

	var dr doResult
	select {
	case <-runCtx.Done():
		cancel()
		_ = pw.CloseWithError(runCtx.Err())
		return nil, runCtx.Err()
	case dr = <-done:
	}
	if dr.err != nil {
		cancel()
		_ = pw.Close()
		return nil, dr.err
	}
	res := dr.res
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		_ = res.Body.Close()
		cancel()
		_ = pw.Close()
		slog.Warn("agent run HTTP error",
			"model", payload.ModelID,
			"origin", origin,
			"status", res.StatusCode,
			"body", truncateForLog(body, 512),
		)
		return nil, WithModelID(classifyHTTP(res.StatusCode, strings.TrimSpace(string(body))), payload.ModelID)
	}
	slog.Debug("agent run connected", "model", payload.ModelID, "origin", origin, "status", res.StatusCode)

	out := make(chan StreamEvent, 32)
	rc := &RunControl{
		Events: out,
		cancel: cancel,
		pw:     pw,
	}
	rc.writeFrame = func(msg *cursorProto.AgentClientMessage) error {
		b, err := proto.Marshal(msg)
		if err != nil {
			return err
		}
		rc.writeMu.Lock()
		defer rc.writeMu.Unlock()
		_, err = pw.Write(FrameConnect(b, flagRaw))
		return err
	}

	hbCtx, hbCancel := context.WithCancel(runCtx)
	go func() {
		t := time.NewTicker(heartbeatEvery)
		defer t.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-t.C:
				_ = rc.writeFrame(&cursorProto.AgentClientMessage{
					Message: &cursorProto.AgentClientMessage_ClientHeartbeat{
						ClientHeartbeat: &cursorProto.ClientHeartbeat{},
					},
				})
			}
		}
	}()

	var onMcp func(PendingExec) error
	if bridgeTools {
		onMcp = func(pe PendingExec) error {
			rc.trackPending(pe)
			out <- StreamEvent{ToolCall: &pe}
			return nil
		}
	}

	go func() {
		defer close(out)
		defer hbCancel()
		defer res.Body.Close()
		defer pw.Close()
		defer cancel()

		idle := time.NewTimer(runIdleTime)
		defer idle.Stop()

		sawTurnEnded := false
		for {
			type frameResult struct {
				f   connectFrame
				err error
			}
			ch := make(chan frameResult, 1)
			go func() {
				f, err := readConnectFrame(res.Body)
				ch <- frameResult{f: f, err: err}
			}()

			var fr frameResult
			select {
			case <-runCtx.Done():
				out <- StreamEvent{Err: runCtx.Err(), HTTPStatus: res.StatusCode}
				return
			case <-idle.C:
				// Keep the Connect stream alive while parked waiting for OpenAI tool results.
				if len(rc.Pending()) > 0 {
					idle.Reset(runIdleTime)
					continue
				}
				out <- StreamEvent{Err: fmt.Errorf("%w: idle timeout", ErrIncompleteRun), HTTPStatus: res.StatusCode}
				return
			case fr = <-ch:
			}

			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(runIdleTime)

			if fr.err != nil {
				if errors.Is(fr.err, io.EOF) || errors.Is(fr.err, io.ErrUnexpectedEOF) {
					if !sawTurnEnded {
						out <- StreamEvent{Err: ErrIncompleteRun, HTTPStatus: res.StatusCode}
					}
					return
				}
				out <- StreamEvent{Err: fr.err, HTTPStatus: res.StatusCode}
				return
			}

			if fr.f.Flags&flagEndStream != 0 {
				if err := parseConnectEndStream(fr.f.Payload); err != nil {
					out <- StreamEvent{Err: WithModelID(err, payload.ModelID), HTTPStatus: res.StatusCode}
				}
				return
			}

			payloadBytes := fr.f.Payload
			if fr.f.Flags&flagGzip != 0 {
				out <- StreamEvent{Err: fmt.Errorf("gzip connect frames not supported"), HTTPStatus: res.StatusCode}
				return
			}

			var serverMsg cursorProto.AgentServerMessage
			if err := proto.Unmarshal(payloadBytes, &serverMsg); err != nil {
				continue
			}

			switch m := serverMsg.Message.(type) {
			case *cursorProto.AgentServerMessage_KvServerMessage:
				if err := handleKV(m.KvServerMessage, payload.BlobStore, rc.writeFrame); err != nil {
					out <- StreamEvent{Err: err, HTTPStatus: res.StatusCode}
					return
				}
			case *cursorProto.AgentServerMessage_ExecServerMessage:
				if err := handleExec(m.ExecServerMessage, payload.Tools, rc.writeFrame, onMcp); err != nil {
					out <- StreamEvent{Err: err, HTTPStatus: res.StatusCode}
					return
				}
			case *cursorProto.AgentServerMessage_InteractionQuery:
				if err := handleInteractionQuery(m.InteractionQuery, rc.writeFrame); err != nil {
					out <- StreamEvent{Err: err, HTTPStatus: res.StatusCode}
					return
				}
			case *cursorProto.AgentServerMessage_ConversationCheckpointUpdate:
				if state := m.ConversationCheckpointUpdate; state != nil {
					if td := state.GetTokenDetails(); td != nil {
						rc.notePromptTokens(int(td.GetUsedTokens()))
					}
				}
			case *cursorProto.AgentServerMessage_InteractionUpdate:
				evs, ended := interactionEvents(m.InteractionUpdate, rc)
				for _, ev := range evs {
					out <- ev
				}
				if ended {
					sawTurnEnded = true
					out <- StreamEvent{TurnEnded: true}
					return
				}
			}
		}
	}()

	return rc, nil
}

func interactionEvents(update *cursorProto.InteractionUpdate, rc *RunControl) (events []StreamEvent, turnEnded bool) {
	if update == nil {
		return nil, false
	}
	if td := update.GetTextDelta(); td != nil && td.Text != "" {
		events = append(events, StreamEvent{Text: td.Text})
	}
	if th := update.GetThinkingDelta(); th != nil && th.Text != "" {
		events = append(events, StreamEvent{Text: th.Text, Thinking: true})
	}
	if delta := update.GetTokenDelta(); delta != nil {
		rc.noteCompletionTokens(int(delta.GetTokens()))
	}
	if update.GetTurnEnded() != nil {
		return events, true
	}
	return events, false
}

func handleKV(msg *cursorProto.KvServerMessage, store map[string][]byte, write func(*cursorProto.AgentClientMessage) error) error {
	if msg == nil {
		return nil
	}
	switch m := msg.Message.(type) {
	case *cursorProto.KvServerMessage_GetBlobArgs:
		if m.GetBlobArgs == nil {
			return nil
		}
		key := hex.EncodeToString(m.GetBlobArgs.GetBlobId())
		data := store[key]
		result := &cursorProto.GetBlobResult{}
		if len(data) > 0 {
			result.BlobData = append([]byte(nil), data...)
		}
		return write(&cursorProto.AgentClientMessage{
			Message: &cursorProto.AgentClientMessage_KvClientMessage{
				KvClientMessage: &cursorProto.KvClientMessage{
					Id: msg.Id,
					Message: &cursorProto.KvClientMessage_GetBlobResult{
						GetBlobResult: result,
					},
				},
			},
		})
	case *cursorProto.KvServerMessage_SetBlobArgs:
		if m.SetBlobArgs == nil {
			return nil
		}
		id := m.SetBlobArgs.GetBlobId()
		data := m.SetBlobArgs.GetBlobData()
		store[hex.EncodeToString(id)] = append([]byte(nil), data...)
		return write(&cursorProto.AgentClientMessage{
			Message: &cursorProto.AgentClientMessage_KvClientMessage{
				KvClientMessage: &cursorProto.KvClientMessage{
					Id: msg.Id,
					Message: &cursorProto.KvClientMessage_SetBlobResult{
						SetBlobResult: &cursorProto.SetBlobResult{},
					},
				},
			},
		})
	}
	return nil
}

// CollectText runs a chat and buffers the full assistant text (non-streaming helper).
func (c *Client) CollectText(ctx context.Context, accessToken string, payload *RunPayload) (string, error) {
	ch, err := c.RunChat(ctx, accessToken, payload)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for ev := range ch {
		if ev.Err != nil {
			return b.String(), ev.Err
		}
		if ev.TurnEnded {
			return b.String(), nil
		}
		if ev.Text == "" {
			continue
		}
		// Thinking deltas are kept as plain content (no <think> wrappers).
		b.WriteString(ev.Text)
	}
	return b.String(), ErrIncompleteRun
}
