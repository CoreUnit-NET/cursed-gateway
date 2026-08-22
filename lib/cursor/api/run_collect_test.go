package cursor_api_sdk

import (
	"errors"
	"testing"
)

func TestCollectVisibleTextSkipsThinking(t *testing.T) {
	ch := make(chan StreamEvent, 4)
	ch <- StreamEvent{Text: "secret plan", Thinking: true}
	ch <- StreamEvent{Text: "hello"}
	ch <- StreamEvent{Text: " world"}
	ch <- StreamEvent{TurnEnded: true}
	close(ch)

	got, err := collectVisibleText(ch)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello world" {
		t.Fatalf("got %q, want %q", got, "hello world")
	}
}

func TestCollectVisibleTextIncomplete(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Text: "partial"}
	close(ch)

	got, err := collectVisibleText(ch)
	if !errors.Is(err, ErrIncompleteRun) {
		t.Fatalf("err = %v, want ErrIncompleteRun", err)
	}
	if got != "partial" {
		t.Fatalf("got %q", got)
	}
}

func TestCollectVisibleTextThinkingOnly(t *testing.T) {
	ch := make(chan StreamEvent, 2)
	ch <- StreamEvent{Text: "secret", Thinking: true}
	ch <- StreamEvent{TurnEnded: true}
	close(ch)

	got, err := collectVisibleText(ch)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestCollectVisibleTextPropagatesError(t *testing.T) {
	ch := make(chan StreamEvent, 2)
	ch <- StreamEvent{Text: "before"}
	ch <- StreamEvent{Err: errors.New("boom")}
	close(ch)

	got, err := collectVisibleText(ch)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v, want boom", err)
	}
	if got != "before" {
		t.Fatalf("got %q on error, want partial before", got)
	}
}
