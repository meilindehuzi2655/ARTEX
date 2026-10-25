package agent

import "testing"

// TestConfigFromAnthropicBaseURL locks the anthropic base-URL normalization:
// the provider appends "/v1/messages" itself, so every accepted input form must
// resolve to a base that does not already contain "/v1" (otherwise the request
// path becomes "/v1/v1/messages" and the gateway returns 404).
func TestConfigFromAnthropicBaseURL(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://gw.example", "https://gw.example"},
		{"https://gw.example/", "https://gw.example"},
		{"https://gw.example/v1", "https://gw.example"},
		{"https://gw.example/v1/", "https://gw.example"},
		{"https://gw.example/v1/messages", "https://gw.example"},
		{"https://gw.example/api/v1", "https://gw.example/api"},
		{"https://gw.example/api/v1/messages", "https://gw.example/api"},
	} {
		c := ConfigFrom("anthropic", "claude-x", tc.in, "sk-x", "")
		if c.BaseURL != tc.want {
			t.Errorf("ConfigFrom(%q).BaseURL=%q, want %q", tc.in, c.BaseURL, tc.want)
		}
	}
}

// TestConfigFromOpenAIKeepsV1Base guards the counterpart: OpenAI-compatible
// gateways expect the "/v1" base (the provider appends only "/chat/completions").
func TestConfigFromOpenAIKeepsV1Base(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://gw.example/v1", "https://gw.example/v1"},
		{"https://gw.example/v1/chat/completions", "https://gw.example/v1"},
	} {
		c := ConfigFrom("openai", "gpt-4o", tc.in, "sk-x", "")
		if c.BaseURL != tc.want {
			t.Errorf("ConfigFrom(%q).BaseURL=%q, want %q", tc.in, c.BaseURL, tc.want)
		}
	}
}
