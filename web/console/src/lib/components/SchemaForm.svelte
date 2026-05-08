<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import Input from './Input.svelte';
  import Select from './Select.svelte';
  import Checkbox from './Checkbox.svelte';
  import Textarea from './Textarea.svelte';
  import Badge from './Badge.svelte';

  /**
   * SchemaForm renders a JSON Schema (Draft 7-ish, the subset MCP tools
   * use) as a form. Output is bound to `value` as a plain object, ready
   * for JSON.stringify on submit.
   *
   * Supported types: object (top-level), string, number/integer, boolean,
   * enum (string), array of primitives. Nested objects collapse to a JSON
   * textarea so the operator never sees an empty form.
   *
   * The component does NOT validate exhaustively — Ajv-style validation
   * is intentionally out of scope for the V1 playground; required-field
   * marking is the only client-side check.
   */
  export let schema: SchemaNode | null = null;
  export let value: Record<string, unknown> = {};
  export let disabled = false;

  type SchemaNode = {
    type?: string | string[];
    properties?: Record<string, SchemaNode>;
    required?: string[];
    enum?: unknown[];
    items?: SchemaNode;
    description?: string;
    title?: string;
    default?: unknown;
    minimum?: number;
    maximum?: number;
  };

  const dispatch = createEventDispatcher<{ change: Record<string, unknown> }>();

  $: properties = schema?.properties ?? {};
  $: requiredKeys = new Set(schema?.required ?? []);
  $: fieldEntries = Object.entries(properties);

  // Initialise missing keys with schema defaults so forms render predictably.
  $: if (schema) {
    let touched = false;
    const next = { ...value };
    for (const [k, prop] of Object.entries(properties)) {
      if (next[k] === undefined && prop.default !== undefined) {
        next[k] = prop.default;
        touched = true;
      }
    }
    if (touched) {
      value = next;
      dispatch('change', value);
    }
  }

  function update(key: string, v: unknown) {
    value = { ...value, [key]: v };
    dispatch('change', value);
  }

  function asString(v: unknown): string {
    if (v === undefined || v === null) return '';
    if (typeof v === 'object') return JSON.stringify(v);
    return String(v);
  }

  function inferType(prop: SchemaNode): string {
    if (Array.isArray(prop.type)) {
      const t = prop.type.find((x) => x !== 'null');
      return t ?? 'string';
    }
    return prop.type ?? 'string';
  }

  function selectOptions(prop: SchemaNode): Array<{ label: string; value: string }> {
    const enumValues = (prop.enum ?? []) as Array<string | number | boolean>;
    return enumValues.map((v) => ({ label: String(v), value: String(v) }));
  }

  function arrayPlaceholder(prop: SchemaNode): string {
    const itemType = prop.items?.type ?? 'string';
    if (itemType === 'string') return '["one", "two"]';
    if (itemType === 'number' || itemType === 'integer') return '[1, 2, 3]';
    return '[ ... ]';
  }

  function parseArrayInput(raw: string): unknown[] | null {
    if (!raw.trim()) return [];
    try {
      const parsed = JSON.parse(raw);
      return Array.isArray(parsed) ? parsed : null;
    } catch {
      return null;
    }
  }

  function parseObjectInput(raw: string): Record<string, unknown> | null {
    if (!raw.trim()) return {};
    try {
      const parsed = JSON.parse(raw);
      return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : null;
    } catch {
      return null;
    }
  }

  function onSelectChange(key: string, ev: Event) {
    const target = ev.currentTarget as HTMLSelectElement;
    update(key, target.value);
  }
  function onCheckboxChange(key: string, ev: Event) {
    const target = ev.currentTarget as HTMLInputElement;
    update(key, target.checked);
  }
  function onNumberInput(key: string, ev: Event) {
    const target = ev.currentTarget as HTMLInputElement;
    update(key, target.value === '' ? undefined : Number(target.value));
  }
  function onStringInput(key: string, ev: Event) {
    const target = ev.currentTarget as HTMLInputElement;
    update(key, target.value);
  }
  function onArrayInput(key: string, ev: Event) {
    const target = ev.currentTarget as HTMLTextAreaElement;
    const parsed = parseArrayInput(target.value);
    if (parsed !== null) update(key, parsed);
  }
  function onObjectInput(key: string, ev: Event) {
    const target = ev.currentTarget as HTMLTextAreaElement;
    const parsed = parseObjectInput(target.value);
    if (parsed !== null) update(key, parsed);
  }

  const objectPlaceholder = '{ "key": "value" }';
</script>

{#if !schema || fieldEntries.length === 0}
  <p class="empty">No input parameters defined for this entry.</p>
{:else}
  <div class="grid">
    {#each fieldEntries as [key, prop] (key)}
      {@const t = inferType(prop)}
      {@const enumOpts = selectOptions(prop)}
      {@const isRequired = requiredKeys.has(key)}
      {@const labelText = prop.title ?? key}
      <div class="field">
        {#if enumOpts.length > 0}
          <Select
            label={labelText}
            value={asString(value[key])}
            options={enumOpts}
            on:change={(e) => onSelectChange(key, e)}
            {disabled}
            required={isRequired}
            hint={prop.description}
          />
        {:else if t === 'boolean'}
          <Checkbox
            label={labelText}
            checked={Boolean(value[key])}
            on:change={(e) => onCheckboxChange(key, e)}
            {disabled}
          />
          {#if prop.description}
            <p class="hint">{prop.description}</p>
          {/if}
        {:else if t === 'number' || t === 'integer'}
          <Input
            label={labelText}
            type="number"
            value={asString(value[key])}
            on:input={(e) => onNumberInput(key, e)}
            {disabled}
            required={isRequired}
            hint={prop.description}
          />
        {:else if t === 'array'}
          <Textarea
            label={labelText}
            value={asString(value[key]) || ''}
            placeholder={arrayPlaceholder(prop)}
            on:input={(e) => onArrayInput(key, e)}
            rows={3}
            mono
            hint={prop.description ?? 'JSON array'}
          />
        {:else if t === 'object'}
          <Textarea
            label={labelText}
            value={asString(value[key]) || ''}
            placeholder={objectPlaceholder}
            on:input={(e) => onObjectInput(key, e)}
            rows={4}
            mono
            hint={prop.description ?? 'JSON object'}
          />
        {:else}
          <Input
            label={labelText}
            value={asString(value[key])}
            on:input={(e) => onStringInput(key, e)}
            {disabled}
            required={isRequired}
            hint={prop.description}
          />
        {/if}
        {#if isRequired}
          <Badge tone="warning" size="sm">required</Badge>
        {/if}
      </div>
    {/each}
  </div>
{/if}

<style>
  .grid {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  .empty {
    color: var(--color-text-muted);
    font-size: var(--font-size-body-sm);
    margin: 0;
  }
  .hint {
    font-size: var(--font-size-label);
    color: var(--color-text-tertiary);
    margin: 0;
  }
</style>
