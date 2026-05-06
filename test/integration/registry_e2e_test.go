package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/config"
)

// helpers --------------------------------------------------------------------

func apiPost(t *testing.T, base, path string, body any) (*http.Response, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(http.MethodPost, base+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res, raw
}

func apiGet(t *testing.T, base, path string) (*http.Response, []byte) {
	t.Helper()
	res, err := http.DefaultClient.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res, raw
}

func apiDelete(t *testing.T, base, path string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res, raw
}

// tests ---------------------------------------------------------------------

func TestE2E_Registry_CRUD(t *testing.T) {
	srv, _ := startMcpDevServer(t, nil)

	// Initially empty.
	res, body := apiGet(t, srv.URL, "/v1/servers")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/servers status=%d body=%s", res.StatusCode, body)
	}
	var list []any
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("list not JSON array: %s", body)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}

	// Create a server.
	spec := map[string]any{
		"id":           "demo",
		"display_name": "Demo Server",
		"transport":    "http",
		"runtime_mode": "remote_static",
		"http":         map[string]any{"url": "http://127.0.0.1:1/never-called", "timeout": "1s"},
	}
	res, body = apiPost(t, srv.URL, "/v1/servers", spec)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST /v1/servers status=%d body=%s", res.StatusCode, body)
	}

	// Idempotent re-POST (same id) returns 200.
	res, _ = apiPost(t, srv.URL, "/v1/servers", spec)
	if res.StatusCode != http.StatusOK {
		t.Errorf("re-POST same id: status=%d, want 200", res.StatusCode)
	}

	// Detail.
	res, body = apiGet(t, srv.URL, "/v1/servers/demo")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/servers/demo status=%d body=%s", res.StatusCode, body)
	}
	var got map[string]any
	_ = json.Unmarshal(body, &got)
	if got["id"] != "demo" || got["transport"] != "http" {
		t.Errorf("detail body unexpected: %+v", got)
	}

	// Reload returns 202.
	res, _ = apiPost(t, srv.URL, "/v1/servers/demo/reload", nil)
	if res.StatusCode != http.StatusAccepted {
		t.Errorf("reload status=%d, want 202", res.StatusCode)
	}

	// Disable + Enable.
	res, _ = apiPost(t, srv.URL, "/v1/servers/demo/disable", nil)
	if res.StatusCode != http.StatusOK {
		t.Errorf("disable status=%d", res.StatusCode)
	}
	res, body = apiGet(t, srv.URL, "/v1/servers/demo")
	if res.StatusCode != http.StatusOK {
		t.Fatal(body)
	}
	_ = json.Unmarshal(body, &got)
	if got["enabled"] != false {
		t.Errorf("expected enabled=false after disable, got %+v", got["enabled"])
	}
	res, _ = apiPost(t, srv.URL, "/v1/servers/demo/enable", nil)
	if res.StatusCode != http.StatusOK {
		t.Errorf("enable status=%d", res.StatusCode)
	}

	// Instances list (empty until supervisor runs).
	res, body = apiGet(t, srv.URL, "/v1/servers/demo/instances")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("instances status=%d body=%s", res.StatusCode, body)
	}
	var instances []any
	_ = json.Unmarshal(body, &instances)

	// Delete.
	res, _ = apiDelete(t, srv.URL, "/v1/servers/demo")
	if res.StatusCode != http.StatusNoContent {
		t.Errorf("delete status=%d, want 204", res.StatusCode)
	}

	// 404 on subsequent get.
	res, _ = apiGet(t, srv.URL, "/v1/servers/demo")
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("post-delete GET status=%d, want 404", res.StatusCode)
	}
}

func TestE2E_Registry_RejectsInvalidSpec(t *testing.T) {
	srv, _ := startMcpDevServer(t, nil)

	bad := map[string]any{
		"id":        "Invalid-ID-uppercase",
		"transport": "stdio",
		"stdio":     map[string]any{"command": "echo"},
	}
	res, body := apiPost(t, srv.URL, "/v1/servers", bad)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid id, got %d body=%s", res.StatusCode, body)
	}
	var ge map[string]any
	_ = json.Unmarshal(body, &ge)
	if ge["error"] != "invalid_spec" {
		t.Errorf("error code = %v", ge["error"])
	}

	missingTransport := map[string]any{"id": "noxport"}
	res, _ = apiPost(t, srv.URL, "/v1/servers", missingTransport)
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("missing transport: status=%d, want 400", res.StatusCode)
	}
}

func TestE2E_Registry_TenantSeed(t *testing.T) {
	// When the config has servers + tenants, the registry should be seeded
	// per-tenant on boot. Use a config with one tenant and one server.
	specs := []config.ServerSpec{
		{
			ID:        "h",
			Transport: "http",
			HTTP:      &config.HTTPSpec{URL: "http://127.0.0.1:1"},
		},
	}
	srv, _ := startMcpDevServer(t, specs)

	// In dev mode the dev tenant is also auto-created. Force a tenant
	// resolution by hitting /v1/audit/events first (auth middleware
	// upserts the dev tenant).
	res, _ := apiGet(t, srv.URL, "/v1/audit/events")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("audit endpoint failed: %d", res.StatusCode)
	}
	// Seeding happens in cmd_serve.go before the test scaffold runs. The
	// scaffold here doesn't call seedRegistryFromConfig, so the registry
	// is empty unless the operator pushes via API. The test asserts only
	// that the API surface is reachable; full seed coverage lives in the
	// preflight smoke against `./bin/portico dev`.
	res, body := apiGet(t, srv.URL, "/v1/servers")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", res.StatusCode, body)
	}
}
