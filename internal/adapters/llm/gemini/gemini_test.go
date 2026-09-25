package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, status int, body string) (*httptest.Server, *http.Request, *[]byte) {
	t.Helper()
	var got http.Request
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = *r.Clone(context.Background())
		raw, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &got, &raw
}

const okBody = `{"candidates":[{"content":{"parts":[{"text":"{\"summary\":\"hola\"}"}]},"finishReason":"STOP"}]}`

func TestGenerateSendsThePromptsAndReturnsTheText(t *testing.T) {
	srv, req, raw := serve(t, 200, okBody)
	c := New("secret-key", "gemini-test", WithBaseURL(srv.URL))

	text, err := c.Generate(context.Background(), "sistema", "usuario")
	if err != nil {
		t.Fatal(err)
	}
	if text != `{"summary":"hola"}` {
		t.Errorf("text = %q", text)
	}
	if req.Method != "POST" || req.URL.Path != "/models/gemini-test:generateContent" {
		t.Errorf("%s %s", req.Method, req.URL.Path)
	}
	if req.Header.Get("x-goog-api-key") != "secret-key" || req.URL.RawQuery != "" {
		t.Errorf("the key must travel in the header, not the URL: header %q, query %q", req.Header.Get("x-goog-api-key"), req.URL.RawQuery)
	}
	var sent struct {
		SystemInstruction struct{ Parts []struct{ Text string } }
		Contents          []struct {
			Role  string
			Parts []struct{ Text string }
		}
		GenerationConfig struct {
			Temperature        float64
			ResponseMimeType   string
			ResponseJsonSchema map[string]any
		}
	}
	if err := json.Unmarshal(*raw, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.SystemInstruction.Parts[0].Text != "sistema" || sent.Contents[0].Role != "user" || sent.Contents[0].Parts[0].Text != "usuario" {
		t.Errorf("prompts not sent as given: %s", *raw)
	}
	if sent.GenerationConfig.ResponseMimeType != "application/json" || sent.GenerationConfig.Temperature != 0 {
		t.Errorf("generation config = %+v", sent.GenerationConfig)
	}
	if props, _ := sent.GenerationConfig.ResponseJsonSchema["properties"].(map[string]any); props["summary"] == nil || props["investigation_steps"] == nil {
		t.Errorf("schema not sent: %v", sent.GenerationConfig.ResponseJsonSchema)
	}
}

func TestGenerateReportsWhatWentWrong(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"quota", 429, `{"error":{"code":429,"message":"quota exceeded","status":"RESOURCE_EXHAUSTED"}}`, "429 RESOURCE_EXHAUSTED: quota exceeded"},
		{"error page", 502, `<html>bad gateway</html>`, "answered 502"},
		{"not json", 200, `hello`, "not JSON"},
		{"blocked", 200, `{"promptFeedback":{"blockReason":"SAFETY"}}`, "blocked the prompt: SAFETY"},
		{"no candidates", 200, `{"candidates":[]}`, "without a candidate"},
		{"no parts", 200, `{"candidates":[{"content":{"parts":[]}}]}`, "without a candidate"},
		{"cut short", 200, `{"candidates":[{"content":{"parts":[{"text":"{"}]},"finishReason":"MAX_TOKENS"}]}`, "stopped early: MAX_TOKENS"},
	}
	for _, tc := range cases {
		srv, _, _ := serve(t, tc.status, tc.body)
		_, err := New("k", "m", WithBaseURL(srv.URL)).Generate(context.Background(), "s", "u")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to contain %q", tc.name, err, tc.want)
		}
	}
}

func TestGenerateHonoursTheContext(t *testing.T) {
	srv, _, _ := serve(t, 200, okBody)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New("k", "m", WithBaseURL(srv.URL)).Generate(ctx, "s", "u"); err == nil {
		t.Error("a cancelled context still made the call")
	}
}

func TestClientNamesItself(t *testing.T) {
	c := New("k", "gemini-3.8-flash")
	if c.Provider() != "gemini" || c.Model() != "gemini-3.8-flash" {
		t.Errorf("%q %q", c.Provider(), c.Model())
	}
}
