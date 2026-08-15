package cursor_api_sdk

// Usage is the OpenAI chat/completions token meter (Path A / otto).
// prompt = max(checkpoint.usedTokens); completion = Σ token_delta; total = sum.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// usageAcc accumulates Cursor Path A usage for one AgentService/Run.
type usageAcc struct {
	prompt     int
	completion int
}

func (a *usageAcc) notePrompt(used int) {
	if a == nil || used <= 0 {
		return
	}
	if used > a.prompt {
		a.prompt = used
	}
}

func (a *usageAcc) noteCompletion(delta int) {
	if a == nil || delta <= 0 {
		return
	}
	a.completion += delta
}

func (a *usageAcc) snapshot() Usage {
	if a == nil {
		return Usage{}
	}
	prompt := a.prompt
	if prompt < 0 {
		prompt = 0
	}
	completion := a.completion
	if completion < 0 {
		completion = 0
	}
	return Usage{
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      prompt + completion,
	}
}
