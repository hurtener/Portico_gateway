package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// verdict is the outcome of a single conformance check.
type verdict string

const (
	verdictOK   verdict = "ok"
	verdictSkip verdict = "skip"
	verdictFail verdict = "fail"
)

// confCheck runs one conformance check, prints its result line, and returns a
// verdict. A non-nil error means the target is unreachable (a transport
// failure), which aborts the whole suite.
type confCheck func(ctx context.Context, client *http.Client, target, token, model string) (verdict, error)

func runConformance(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("conformance", flag.ExitOnError)
	suite := fs.String("suite", "", "conformance suite to run (openai)")
	target := fs.String("target", "", "base URL of running gateway (required)")
	token := fs.String("token", "", "JWT bearer token (optional)")
	model := fs.String("model", "gpt-4o", "model alias to use for chat/embedding requests")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *suite != "openai" {
		return errors.New("unknown suite: " + *suite)
	}
	if *target == "" {
		return errors.New("--target is required")
	}
	return runOpenAIConformance(ctx, strings.TrimRight(*target, "/"), *token, *model)
}

func runOpenAIConformance(ctx context.Context, target, token, model string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	checks := []confCheck{
		checkModels,
		checkChat,
		checkChatUnknownModel,
		checkChatMalformed,
		checkEmbeddings,
	}
	var ok, skip, fail int
	for _, check := range checks {
		v, err := check(ctx, client, target, token, model)
		if err != nil {
			return err
		}
		switch v {
		case verdictOK:
			ok++
		case verdictSkip:
			skip++
		case verdictFail:
			fail++
		}
	}
	fmt.Printf("conformance: %d ok, %d skip, %d fail\n", ok, skip, fail)
	if fail > 0 {
		return errors.New("conformance failed")
	}
	return nil
}

// checkModels asserts GET /v1/models returns the OpenAI list envelope.
func checkModels(ctx context.Context, client *http.Client, target, token, _ string) (verdict, error) {
	status, raw, err := doJSON(ctx, client, "GET", target+"/v1/models", token, nil)
	if err != nil {
		return verdictFail, fmt.Errorf("models: transport error: %w", err)
	}
	if status != 200 {
		fmt.Printf("models: FAIL - status %d\n", status)
		return verdictFail, nil
	}
	parsed, perr := parseObject(raw)
	if perr != nil {
		fmt.Println("models: FAIL - invalid JSON")
		return verdictFail, nil
	}
	if parsed["object"] == "list" && isArray(parsed["data"]) {
		fmt.Println("models: OK")
		return verdictOK, nil
	}
	fmt.Println("models: FAIL - unexpected response shape")
	return verdictFail, nil
}

// checkChat asserts a chat completion either succeeds with the right envelope
// or fails with a well-formed error (SKIP — no usable upstream configured).
func checkChat(ctx context.Context, client *http.Client, target, token, model string) (verdict, error) {
	body := map[string]any{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}
	status, raw, err := doJSON(ctx, client, "POST", target+"/v1/chat/completions", token, body)
	if err != nil {
		return verdictFail, fmt.Errorf("chat: transport error: %w", err)
	}
	return classifyDispatch("chat", status, raw, func(p map[string]any) bool {
		return p["object"] != nil && isArray(p["choices"]) && p["usage"] != nil
	}), nil
}

// checkEmbeddings mirrors checkChat for the embeddings surface.
func checkEmbeddings(ctx context.Context, client *http.Client, target, token, model string) (verdict, error) {
	body := map[string]any{"model": model, "input": "hi"}
	status, raw, err := doJSON(ctx, client, "POST", target+"/v1/embeddings", token, body)
	if err != nil {
		return verdictFail, fmt.Errorf("embeddings: transport error: %w", err)
	}
	return classifyDispatch("embeddings", status, raw, func(p map[string]any) bool {
		return isArray(p["data"]) && p["usage"] != nil
	}), nil
}

// checkChatUnknownModel asserts an unknown model yields an error envelope —
// validates the error path without needing a real upstream.
func checkChatUnknownModel(ctx context.Context, client *http.Client, target, token, _ string) (verdict, error) {
	body := map[string]any{
		"model":    "__definitely_not_a_real_model__",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}
	status, raw, err := doJSON(ctx, client, "POST", target+"/v1/chat/completions", token, body)
	if err != nil {
		return verdictFail, fmt.Errorf("chat (unknown model): transport error: %w", err)
	}
	return expectErrorEnvelope("chat (unknown model)", status, raw), nil
}

// checkChatMalformed asserts a malformed body yields a 4xx error envelope.
func checkChatMalformed(ctx context.Context, client *http.Client, target, token, _ string) (verdict, error) {
	status, raw, err := doRaw(ctx, client, "POST", target+"/v1/chat/completions", token, []byte("{"))
	if err != nil {
		return verdictFail, fmt.Errorf("chat (malformed): transport error: %w", err)
	}
	return expectErrorEnvelope("chat (malformed)", status, raw), nil
}

// classifyDispatch handles the shared "200 success-shape OR well-formed error
// → SKIP" logic for the chat + embeddings dispatch checks.
func classifyDispatch(name string, status int, raw []byte, okShape func(map[string]any) bool) verdict {
	if status == 200 {
		parsed, err := parseObject(raw)
		if err != nil {
			fmt.Printf("%s: FAIL - invalid JSON\n", name)
			return verdictFail
		}
		if okShape(parsed) {
			fmt.Printf("%s: OK\n", name)
			return verdictOK
		}
		fmt.Printf("%s: FAIL - unexpected success response shape\n", name)
		return verdictFail
	}
	if status >= 400 && hasErrorKey(raw) {
		fmt.Printf("%s: SKIP - no usable model/upstream (%d)\n", name, status)
		return verdictSkip
	}
	fmt.Printf("%s: FAIL - non-2xx without error envelope (%d)\n", name, status)
	return verdictFail
}

// expectErrorEnvelope passes when the response is a 4xx/5xx with an {error:…} body.
func expectErrorEnvelope(name string, status int, raw []byte) verdict {
	if status >= 400 && hasErrorKey(raw) {
		fmt.Printf("%s: OK\n", name)
		return verdictOK
	}
	fmt.Printf("%s: FAIL - expected 4xx/5xx with error envelope, got %d\n", name, status)
	return verdictFail
}

func doJSON(ctx context.Context, client *http.Client, method, url, token string, body any) (int, []byte, error) {
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		bodyBytes = b
	}
	return doRaw(ctx, client, method, url, token, bodyBytes)
}

func doRaw(ctx context.Context, client *http.Client, method, url, token string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(string(body)))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, raw, nil
}

func parseObject(raw []byte) (map[string]any, error) {
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func isArray(v any) bool {
	_, ok := v.([]any)
	return ok
}

func hasErrorKey(raw []byte) bool {
	parsed, err := parseObject(raw)
	if err != nil {
		return false
	}
	_, ok := parsed["error"]
	return ok
}
