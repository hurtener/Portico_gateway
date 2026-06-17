package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunConformance_AllPass(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]string{{"id": "gpt-4o", "object": "model"}},
		})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "invalid json"}})
			return
		}
		model := body["model"]
		if model == "__definitely_not_a_real_model__" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "unknown model"}})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"object": "chat.completion",
			"choices": []map[string]any{
				{"index": 0, "message": map[string]string{"role": "assistant", "content": "hi"}},
			},
			"usage": map[string]int{"total_tokens": 3},
		})
	})
	mux.HandleFunc("/v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]any{{"embedding": []float64{0.1}}},
			"usage":  map[string]int{"total_tokens": 1},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	err := runConformance(ctx, []string{"--suite", "openai", "--target", srv.URL})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestRunConformance_UnknownSuite(t *testing.T) {
	ctx := context.Background()
	err := runConformance(ctx, []string{"--suite", "bogus", "--target", "http://localhost:8080"})
	if err == nil {
		t.Fatal("expected non-nil error for unknown suite")
	}
	if !strings.Contains(err.Error(), "unknown suite") {
		t.Fatalf("expected error about unknown suite, got: %v", err)
	}
}

func TestRunConformance_TargetDown(t *testing.T) {
	ctx := context.Background()
	err := runConformance(ctx, []string{"--suite", "openai", "--target", "http://127.0.0.1:1"})
	if err == nil {
		t.Fatal("expected non-nil error for down target")
	}
}

func TestRunConformance_ChatReturns500WithErrorEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]string{{"id": "gpt-4o", "object": "model"}},
		})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "upstream error"}})
	})
	mux.HandleFunc("/v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "upstream error"}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	err := runConformance(ctx, []string{"--suite", "openai", "--target", srv.URL})
	if err != nil {
		t.Fatalf("expected nil error (SKIPs not FAILs), got: %v", err)
	}
}
