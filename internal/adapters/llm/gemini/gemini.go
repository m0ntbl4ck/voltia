// Package gemini asks Google's Gemini models for the text of an explanation
// through the Generative Language API.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/adapters/llm"
)

const defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"

// maxBody caps how much of a response is read, so a broken endpoint cannot fill memory.
const maxBody = 1 << 20

// Attempts is how many times a call is made when Google answers that it is
// busy or failing for a moment; the wait doubles between them.
const (
	attempts   = 3
	firstDelay = time.Second
)

// Client calls one Gemini model. It implements llm.Generator.
type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
	delay   time.Duration
}

// Option changes how a Client is built.
type Option func(*Client)

// WithBaseURL points the client at another server, for tests.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// WithHTTPClient replaces the default HTTP client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithRetryDelay sets the first wait between attempts, for tests.
func WithRetryDelay(d time.Duration) Option { return func(c *Client) { c.delay = d } }

// New returns a client for model, authenticating with apiKey.
func New(apiKey, model string, opts ...Option) *Client {
	c := &Client{apiKey: apiKey, model: model, baseURL: defaultBaseURL, http: &http.Client{}, delay: firstDelay}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) Provider() string { return "gemini" }
func (c *Client) Model() string    { return c.model }

type part struct {
	Text string `json:"text"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type request struct {
	SystemInstruction content          `json:"systemInstruction"`
	Contents          []content        `json:"contents"`
	GenerationConfig  generationConfig `json:"generationConfig"`
}

type generationConfig struct {
	Temperature      float64        `json:"temperature"`
	ResponseMIMEType string         `json:"responseMimeType"`
	ResponseSchema   map[string]any `json:"responseJsonSchema"`
}

type response struct {
	Candidates []struct {
		Content      content `json:"content"`
		FinishReason string  `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// retryable reports statuses that usually pass on their own: too many requests
// and the server errors Google sends when a model is under load.
func retryable(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// Generate sends the prompts and returns the JSON text of the first answer.
// A busy or failing model is tried again after a short wait, up to attempts times.
func (c *Client) Generate(ctx context.Context, system, user string) (string, error) {
	body, err := json.Marshal(request{
		SystemInstruction: content{Parts: []part{{Text: system}}},
		Contents:          []content{{Role: "user", Parts: []part{{Text: user}}}},
		GenerationConfig:  generationConfig{ResponseMIMEType: "application/json", ResponseSchema: llm.Schema},
	})
	if err != nil {
		return "", err
	}
	wait := c.delay
	for i := 1; ; i++ {
		text, status, err := c.once(ctx, body)
		if err == nil || i == attempts || !retryable(status) {
			return text, err
		}
		select {
		case <-ctx.Done():
			return "", err
		case <-time.After(wait):
		}
		wait *= 2
	}
}

// once makes a single call and also returns the HTTP status, 0 if there was none.
func (c *Client) once(ctx context.Context, body []byte) (string, int, error) {
	endpoint := fmt.Sprintf("%s/models/%s:generateContent", c.baseURL, url.PathEscape(c.model))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.apiKey)

	res, err := c.http.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return "", res.StatusCode, err
	}
	var out response
	// A non-JSON error page still reports its status below.
	jsonErr := json.Unmarshal(raw, &out)
	if res.StatusCode != http.StatusOK {
		if out.Error != nil {
			return "", res.StatusCode, fmt.Errorf("gemini answered %d %s: %s", res.StatusCode, out.Error.Status, out.Error.Message)
		}
		return "", res.StatusCode, fmt.Errorf("gemini answered %d", res.StatusCode)
	}
	if jsonErr != nil {
		return "", res.StatusCode, fmt.Errorf("gemini answer is not JSON: %w", jsonErr)
	}
	if out.PromptFeedback.BlockReason != "" {
		return "", res.StatusCode, fmt.Errorf("gemini blocked the prompt: %s", out.PromptFeedback.BlockReason)
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return "", res.StatusCode, errors.New("gemini answered without a candidate")
	}
	first := out.Candidates[0]
	if first.FinishReason != "" && first.FinishReason != "STOP" {
		return "", res.StatusCode, fmt.Errorf("gemini stopped early: %s", first.FinishReason)
	}
	return first.Content.Parts[0].Text, res.StatusCode, nil
}
