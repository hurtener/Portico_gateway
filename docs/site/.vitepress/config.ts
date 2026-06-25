// VitePress configuration for the Portico tech-docs site.
//
// Static markdown-first generator; the same Vite toolchain the Console
// (web/console) already uses, so the install + build story adds no new
// runtime dependency. GitHub Pages deploys the built site from
// .github/workflows/docs.yml.
//
// The `base` URL is the GitHub Pages path; we read DOCS_BASE from the
// environment so the CI workflow can pin it to "/Portico_gateway/" without
// hard-coding the repo name here (a rename should not need a config edit).
import { defineConfig } from "vitepress";

const base = process.env.DOCS_BASE ?? "/";

export default defineConfig({
  base,
  lang: "en-US",
  title: "Portico",
  description:
    "Portico — the open-source, multi-tenant gateway for agentic traffic. MCP, A2A, and LLM under one governed control plane. One static Go binary.",
  cleanUrls: true,
  // The site fails the build on a dead internal link. Surfacing a 404 before
  // publish is the whole point of having a docs build step in CI.
  ignoreDeadLinks: false,
  head: [["link", { rel: "icon", href: `${base}portico-logo.png` }]],
  themeConfig: {
    siteTitle: "Portico",
    logo: "/portico-logo.svg",
    nav: [
      { text: "Get started", link: "/getting-started/" },
      { text: "Concepts", link: "/concepts/" },
      { text: "Guides", link: "/guides/" },
      {
        text: "Reference",
        items: [
          { text: "Configuration", link: "/reference/configuration" },
          { text: "CLI", link: "/reference/cli" },
          { text: "REST API", link: "/reference/rest-api" },
          { text: "MCP & A2A methods", link: "/reference/mcp-methods" },
          { text: "Roadmap", link: "/reference/roadmap" },
          { text: "RFC-001", link: "/reference/rfc" },
        ],
      },
    ],
    sidebar: {
      "/getting-started/": [
        {
          text: "Get started",
          items: [
            { text: "Overview", link: "/getting-started/" },
            { text: "Installation", link: "/getting-started/installation" },
            { text: "Dev mode", link: "/getting-started/dev-mode" },
            { text: "Your first MCP server", link: "/getting-started/first-mcp-server" },
            { text: "Your first LLM call", link: "/getting-started/first-llm-call" },
            { text: "Console tour", link: "/getting-started/console-tour" },
          ],
        },
      ],
      "/concepts/": [
        {
          text: "Concepts",
          items: [
            { text: "Overview", link: "/concepts/" },
            { text: "Architecture", link: "/concepts/architecture" },
            { text: "Multi-tenancy", link: "/concepts/multi-tenancy" },
          ],
        },
        {
          text: "Consumer governance",
          items: [
            { text: "Agent Profiles", link: "/concepts/agent-profiles" },
            { text: "Virtual Keys", link: "/concepts/virtual-keys" },
            { text: "Hierarchical Budgets", link: "/concepts/hierarchical-budgets" },
            { text: "Authentication & scopes", link: "/concepts/authentication" },
            { text: "Policy", link: "/concepts/policy" },
          ],
        },
        {
          text: "MCP Gateway",
          items: [
            { text: "Overview", link: "/concepts/mcp-gateway" },
            { text: "Northbound (clients)", link: "/concepts/mcp-northbound" },
            { text: "Southbound (servers)", link: "/concepts/mcp-southbound" },
            { text: "Server registry", link: "/concepts/mcp-registry" },
            { text: "Catalog & sessions", link: "/concepts/catalog-and-sessions" },
          ],
        },
        {
          text: "LLM Gateway",
          items: [
            { text: "Overview", link: "/concepts/llm-gateway" },
            { text: "Providers & model catalog", link: "/concepts/llm-providers" },
            { text: "Routing & fallback", link: "/concepts/llm-routing" },
            { text: "Semantic Cache", link: "/concepts/semantic-cache" },
          ],
        },
        {
          text: "A2A",
          items: [
            { text: "Overview", link: "/concepts/a2a" },
            { text: "Bridges & ingestion", link: "/concepts/a2a-bridges" },
          ],
        },
        {
          text: "Skills & Code Mode",
          items: [
            { text: "Skill Packs", link: "/concepts/skill-packs" },
            { text: "Skill sources", link: "/concepts/skill-sources" },
            { text: "Code Mode", link: "/concepts/code-mode" },
            { text: "Token savings", link: "/concepts/code-mode-savings" },
            { text: "Approvals", link: "/concepts/approvals" },
          ],
        },
        {
          text: "Credentials & security",
          items: [
            { text: "Credentials & Vault", link: "/concepts/credentials-vault" },
            { text: "OAuth token exchange", link: "/concepts/oauth-token-exchange" },
            { text: "Security model", link: "/concepts/security-model" },
          ],
        },
        {
          text: "Observability",
          items: [
            { text: "Telemetry", link: "/concepts/observability" },
            { text: "Audit", link: "/concepts/audit" },
            { text: "Drift detection", link: "/concepts/drift-detection" },
            { text: "Playground", link: "/concepts/playground" },
            { text: "Console", link: "/concepts/console" },
          ],
        },
      ],
      "/guides/": [
        {
          text: "Guides",
          items: [
            { text: "Overview", link: "/guides/" },
            { text: "Deployment & configuration", link: "/guides/deployment" },
            { text: "Manage providers, keys & budgets", link: "/guides/manage-providers" },
            { text: "Create an Agent Profile", link: "/guides/create-agent-profile" },
            { text: "Register an MCP server", link: "/guides/register-mcp-server" },
            { text: "Build a Skill Pack", link: "/guides/build-skill-pack" },
            { text: "Use Code Mode", link: "/guides/use-code-mode" },
            { text: "Set up an A2A peer", link: "/guides/setup-a2a-peer" },
          ],
        },
      ],
      "/reference/": [
        {
          text: "Reference",
          items: [
            { text: "Configuration", link: "/reference/configuration" },
            { text: "CLI", link: "/reference/cli" },
            { text: "REST API", link: "/reference/rest-api" },
            { text: "MCP & A2A methods", link: "/reference/mcp-methods" },
            { text: "Roadmap", link: "/reference/roadmap" },
            { text: "RFC-001 — Portico", link: "/reference/rfc" },
          ],
        },
      ],
    },
    socialLinks: [
      { icon: "github", link: "https://github.com/hurtener/Portico_gateway" },
    ],
    footer: {
      message: "Apache-2.0 licensed — see LICENSE.",
      copyright: "© 2026 The Portico Authors. Generated by VitePress.",
    },
    outline: { level: [2, 3] },
    search: { provider: "local" },
  },
});
