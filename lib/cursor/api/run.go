package cursor_api_sdk

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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
	Err        error
	HTTPStatus int
}

// RunChat opens AgentService/Run, handles KV/heartbeats, and emits text events.
// The returned channel is closed when the run finishes.
func (c *Client) RunChat(ctx context.Context, accessToken string, payload *RunPayload) (<-chan StreamEvent, error) {
	if payload == nil || len(payload.RequestBytes) == 0 {
		return nil, fmt.Errorf("run payload is empty")
	}
	origin, err := c.ResolveAgentOrigin(ctx, accessToken)
	if err != nil {
		origin = c.baseURL()
	}

	pr, pw := io.Pipe()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+pathAgentRun, pr)
	if err != nil {
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
		_ = pw.CloseWithError(err)
		return nil, err
	}

	var dr doResult
	select {
	case <-ctx.Done():
		_ = pw.CloseWithError(ctx.Err())
		return nil, ctx.Err()
	case dr = <-done:
	}
	if dr.err != nil {
		_ = pw.Close()
		return nil, dr.err
	}
	res := dr.res
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		_ = res.Body.Close()
		_ = pw.Close()
		return nil, classifyHTTP(res.StatusCode, strings.TrimSpace(string(body)))
	}

	out := make(chan StreamEvent, 32)
	var writeMu sync.Mutex
	writeFrame := func(msg *cursorProto.AgentClientMessage) error {
		b, err := proto.Marshal(msg)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_, err = pw.Write(FrameConnect(b, flagRaw))
		return err
	}

	hbCtx, hbCancel := context.WithCancel(ctx)
	go func() {
		t := time.NewTicker(heartbeatEvery)
		defer t.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-t.C:
				_ = writeFrame(&cursorProto.AgentClientMessage{
					Message: &cursorProto.AgentClientMessage_ClientHeartbeat{
						ClientHeartbeat: &cursorProto.ClientHeartbeat{},
					},
				})
			}
		}
	}()

	go func() {
		defer close(out)
		defer hbCancel()
		defer res.Body.Close()
		defer pw.Close()

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
			case <-ctx.Done():
				out <- StreamEvent{Err: ctx.Err(), HTTPStatus: res.StatusCode}
				return
			case <-idle.C:
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
					out <- StreamEvent{Err: err, HTTPStatus: res.StatusCode}
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
				if err := handleKV(m.KvServerMessage, payload.BlobStore, writeFrame); err != nil {
					out <- StreamEvent{Err: err, HTTPStatus: res.StatusCode}
					return
				}
			case *cursorProto.AgentServerMessage_ExecServerMessage:
				if err := handleExec(m.ExecServerMessage, payload.Tools, writeFrame); err != nil {
					out <- StreamEvent{Err: err, HTTPStatus: res.StatusCode}
					return
				}
			case *cursorProto.AgentServerMessage_InteractionQuery:
				if err := handleInteractionQuery(m.InteractionQuery, writeFrame); err != nil {
					out <- StreamEvent{Err: err, HTTPStatus: res.StatusCode}
					return
				}
			case *cursorProto.AgentServerMessage_InteractionUpdate:
				evs, ended := interactionEvents(m.InteractionUpdate)
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

	return out, nil
}

func interactionEvents(update *cursorProto.InteractionUpdate) (events []StreamEvent, turnEnded bool) {
	if update == nil {
		return nil, false
	}
	if td := update.GetTextDelta(); td != nil && td.Text != "" {
		events = append(events, StreamEvent{Text: td.Text})
	}
	if th := update.GetThinkingDelta(); th != nil && th.Text != "" {
		events = append(events, StreamEvent{Text: th.Text, Thinking: true})
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
	var thinkingOpen bool
	for ev := range ch {
		if ev.Err != nil {
			return b.String(), ev.Err
		}
		if ev.TurnEnded {
			if thinkingOpen {
				b.WriteString("</think>")
			}
			return b.String(), nil
		}
		if ev.Text == "" {
			continue
		}
		if ev.Thinking {
			if !thinkingOpen {
				b.WriteString("<think>")
				thinkingOpen = true
			}
			b.WriteString(ev.Text)
			continue
		}
		if thinkingOpen {
			b.WriteString("</think>")
			thinkingOpen = false
		}
		b.WriteString(ev.Text)
	}
	return b.String(), ErrIncompleteRun
}
