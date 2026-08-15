package cursor_api_sdk

import "testing"

func TestUsageAccPathA(t *testing.T) {
	var a usageAcc

	a.notePrompt(100)
	a.notePrompt(80)  // earlier/smaller must not shrink
	a.notePrompt(150) // keep max
	a.noteCompletion(10)
	a.noteCompletion(5)
	a.noteCompletion(-3) // ignore
	a.notePrompt(0)      // ignore
	a.notePrompt(-1)     // ignore

	got := a.snapshot()
	if got.PromptTokens != 150 {
		t.Fatalf("prompt: got %d want 150", got.PromptTokens)
	}
	if got.CompletionTokens != 15 {
		t.Fatalf("completion: got %d want 15", got.CompletionTokens)
	}
	if got.TotalTokens != 165 {
		t.Fatalf("total: got %d want 165 (prompt+completion, not usedTokens-as-total)", got.TotalTokens)
	}
}

func TestUsageAccEmpty(t *testing.T) {
	var a *usageAcc
	if got := a.snapshot(); got != (Usage{}) {
		t.Fatalf("nil acc snapshot: %+v", got)
	}
	a = &usageAcc{}
	if got := a.snapshot(); got != (Usage{}) {
		t.Fatalf("empty acc snapshot: %+v", got)
	}
}

func TestUsageAccAntiOauthTotal(t *testing.T) {
	// oauth anti-pattern: treat usedTokens as total then prompt = total - completion.
	// Path A keeps usedTokens as prompt only.
	var a usageAcc
	a.notePrompt(200) // context fill
	a.noteCompletion(40)
	got := a.snapshot()
	if got.PromptTokens != 200 || got.CompletionTokens != 40 || got.TotalTokens != 240 {
		t.Fatalf("anti-oauth: got %+v want prompt=200 completion=40 total=240", got)
	}
	if got.PromptTokens == got.TotalTokens-got.CompletionTokens && got.PromptTokens != 200 {
		t.Fatal("unexpected oauth-style prompt derivation")
	}
}
