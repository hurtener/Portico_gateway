/**
 * Custom-provider catalog (Phase 13). One-click templates for OpenAI-compatible
 * endpoints registered through the `custom_openai` driver.
 *
 * IMPORTANT — base_url convention: the Bifrost engine appends exactly
 * `/v1/chat/completions` to the configured base URL (its OpenAI code path with a
 * default base of `https://api.openai.com`). So every `baseUrl` here is the host
 * root such that `{baseUrl}/v1/chat/completions` is the real chat endpoint — no
 * trailing `/v1`, no trailing slash. Providers whose OpenAI path is NOT
 * `{host}/v1/chat/completions` (DeepInfra `/v1/openai/…`, Novita `/v3/openai/…`,
 * Perplexity `/chat/completions` with no `/v1`) are intentionally omitted —
 * they can't route through a plain base-URL swap. Bifrost natives (openai,
 * anthropic, groq, mistral, perplexity, ollama, …) have dedicated drivers in the
 * Driver dropdown and are not duplicated here.
 *
 * Local entries use each project's default port; operators edit the base URL
 * after applying a template if their deployment differs.
 */

export interface ProviderPreset {
  /** Stable id; also the suggested provider name when the name field is empty. */
  id: string;
  label: string;
  baseUrl: string;
  group: 'hosted' | 'local';
  note?: string;
}

export const PROVIDER_PRESETS: ProviderPreset[] = [
  // ── Hosted, OpenAI-compatible ────────────────────────────────────────────
  { id: 'deepseek', label: 'DeepSeek', baseUrl: 'https://api.deepseek.com', group: 'hosted' },
  { id: 'together', label: 'Together AI', baseUrl: 'https://api.together.xyz', group: 'hosted' },
  {
    id: 'fireworks',
    label: 'Fireworks AI',
    baseUrl: 'https://api.fireworks.ai/inference',
    group: 'hosted'
  },
  { id: 'hyperbolic', label: 'Hyperbolic', baseUrl: 'https://api.hyperbolic.xyz', group: 'hosted' },
  {
    id: 'sambanova',
    label: 'SambaNova Cloud',
    baseUrl: 'https://api.sambanova.ai',
    group: 'hosted'
  },
  { id: 'lambda', label: 'Lambda Inference', baseUrl: 'https://api.lambda.ai', group: 'hosted' },
  {
    id: 'moonshot',
    label: 'Moonshot / Kimi',
    baseUrl: 'https://api.moonshot.ai',
    group: 'hosted',
    note: 'China region: https://api.moonshot.cn'
  },
  {
    id: 'parasail',
    label: 'Parasail',
    baseUrl: 'https://api.saas.parasail.io',
    group: 'hosted'
  },

  // ── Self-hosted / local OpenAI servers (default ports; edit to match) ─────
  { id: 'vllm', label: 'vLLM (self-hosted)', baseUrl: 'http://localhost:8000', group: 'local' },
  {
    id: 'sglang',
    label: 'SGLang (self-hosted)',
    baseUrl: 'http://localhost:30000',
    group: 'local'
  },
  { id: 'lmstudio', label: 'LM Studio', baseUrl: 'http://localhost:1234', group: 'local' },
  { id: 'llamacpp', label: 'llama.cpp server', baseUrl: 'http://localhost:8080', group: 'local' },
  { id: 'localai', label: 'LocalAI', baseUrl: 'http://localhost:8080', group: 'local' },
  {
    id: 'tgi',
    label: 'HF Text Generation Inference',
    baseUrl: 'http://localhost:8080',
    group: 'local'
  },
  { id: 'jan', label: 'Jan', baseUrl: 'http://localhost:1337', group: 'local' }
];
