package completion_api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseBoolish(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    bool
		wantErr bool
	}{
		{name: "empty", raw: "", want: false},
		{name: "null", raw: "null", want: false},
		{name: "true", raw: "true", want: true},
		{name: "false", raw: "false", want: false},
		{name: "string true", raw: `"true"`, want: true},
		{name: "string 1", raw: `"1"`, want: true},
		{name: "string yes", raw: `"yes"`, want: true},
		{name: "string no", raw: `"no"`, want: false},
		{name: "number 1", raw: "1", want: true},
		{name: "number 0", raw: "0", want: false},
		{name: "object", raw: `{}`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got bool
			err := parseBoolish(json.RawMessage(tc.raw), &got)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestValidateN(t *testing.T) {
	if err := validateN(nil); err != nil {
		t.Fatalf("nil: %v", err)
	}
	one := 1
	if err := validateN(&one); err != nil {
		t.Fatalf("1: %v", err)
	}
	two := 2
	if err := validateN(&two); err == nil || !strings.Contains(err.Error(), "n must be 1") {
		t.Fatalf("2: %v", err)
	}
}

func TestIncludeStreamUsage(t *testing.T) {
	if includeStreamUsage(nil) {
		t.Fatal("nil opts should be false")
	}
	if includeStreamUsage(&StreamOptions{}) {
		t.Fatal("zero opts should be false")
	}
	if !includeStreamUsage(&StreamOptions{IncludeUsage: true}) {
		t.Fatal("IncludeUsage true should gate on")
	}
}

func TestChatCompletionRequestBoolishStream(t *testing.T) {
	var req ChatCompletionRequest
	if err := json.Unmarshal([]byte(`{"model":"m","messages":[],"stream":"1"}`), &req); err != nil {
		t.Fatal(err)
	}
	if !req.Stream {
		t.Fatal("stream string 1 should parse true")
	}
}
