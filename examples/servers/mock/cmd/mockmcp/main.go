// Command mockmcp is the standalone mock MCP server used by Portico's
// integration tests + the preflight smoke. It speaks MCP over stdio.
//
// Usage:
//
//	mockmcp [--name NAME] [--version VERSION] [--resources] [--prompts]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/hurtener/Portico_gateway/examples/servers/mock"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

func main() {
	name := flag.String("name", "mockmcp", "server name to advertise in initialize")
	version := flag.String("version", "0.1.0", "server version to advertise")
	enableResources := flag.Bool("resources", false, "advertise a small set of resources (Phase 3 fixture)")
	enablePrompts := flag.Bool("prompts", false, "advertise a small set of prompts (Phase 3 fixture)")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	srv := &mock.Server{Name: *name, Version: *version}
	if *enableResources {
		srv.ResourcesOverride = []protocol.Resource{
			{URI: "file:///doc/readme.md", Name: "readme", MimeType: "text/markdown"},
			{URI: "ui://panel.html", Name: "panel", MimeType: "text/html"},
		}
		srv.ResourceTemplatesOverride = []protocol.ResourceTemplate{
			{URITemplate: "file:///doc/{slug}.md", Name: "doc-by-slug"},
		}
		srv.ReadOverride = func(uri string) (*protocol.ReadResourceResult, error) {
			body := "# mock content for " + uri
			mime := "text/markdown"
			if uri == "ui://panel.html" {
				body = "<html><head><title>panel</title></head><body><div>hi from " + *name + "</div></body></html>"
				mime = "text/html"
			}
			return &protocol.ReadResourceResult{Contents: []protocol.ResourceContent{
				{URI: uri, MimeType: mime, Text: body},
			}}, nil
		}
	}
	if *enablePrompts {
		srv.PromptsOverride = []protocol.Prompt{
			{Name: "summarize", Description: "Summarise input"},
			{Name: "review", Description: "Review a diff", Arguments: []protocol.PromptArgument{{Name: "diff", Required: true}}},
		}
		srv.GetPromptOverride = func(promptName string, args map[string]string) (*protocol.GetPromptResult, error) {
			return &protocol.GetPromptResult{
				Description: "rendered " + promptName,
				Messages: []protocol.PromptMessage{
					{Role: "user", Content: protocol.ContentBlock{Type: "text", Text: jsonOrEcho(promptName, args)}},
				},
			}, nil
		}
	}
	if err := srv.Run(ctx, os.Stdin, os.Stdout); err != nil && err != context.Canceled {
		// Errors go to stderr; stdout is the JSON-RPC channel.
		os.Stderr.WriteString("mockmcp: " + err.Error() + "\n")
		os.Exit(1)
	}
}

func jsonOrEcho(name string, args map[string]string) string {
	if len(args) == 0 {
		return name
	}
	b, _ := json.Marshal(args)
	return name + ":" + string(b)
}
