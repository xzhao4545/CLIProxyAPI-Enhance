# Response Model Validation

`response-model-validation` enables upstream response validation for selected
provider and resolved-model pairs. It is an optional root section in both
configuration layouts and supports hot reload.

```yaml
response-model-validation:
  - provider: codex
    label: my-codex-upstream
    models: [gpt-5.4, gpt-5.4-mini]
  - provider: openai-compatible-my-gateway
    models: [gpt-4o]
```

`provider` is the executor's provider key: for example `codex`, `claude`,
`gemini`, or `openai-compatible-<name>` for an OpenAI-compatible configuration.
Optional `label`
restricts the rule to a specific provider configuration's runtime credential
label. Both provider and label must match; omitting `label` covers all credentials
of that provider. Credentials sharing the same label share the rule. Labels
match exactly after trimming whitespace and are case-sensitive.

Configure the desired label on the corresponding API-key/provider entry. It is
preserved in runtime credentials and the raw provider/auth labels in usage
records. OpenAI-compatible entries without a label use their configured name;
Vertex-compatible entries without one use `vertex-apikey`. Display overrides
in `usage.provider-labels` do not select credentials.

`models` lists resolved model IDs after alias and model-pool selection. Provider
and model matching ignore case and surrounding whitespace; model matching also
ignores the request's parenthesized thinking suffix. An empty rule list disables
validation.

## Management UI

The management center exposes these rules in **Configuration → Network → Response
model validation**. Each rule accepts a provider kind/key, an optional configuration
`label`, and one resolved upstream model ID per line. The visual editor uses the
v8 configuration patch API and preserves unknown rule fields and comments. Clearing
the last rule writes an explicit empty list to disable validation. Incomplete
edited rules block saving; concurrent edits to the rule list require review.

## Matching and Rejection

The guard compares the selected model with models explicitly reported by raw
upstream responses, before protocol translation and client-visible alias
rewriting. It calls `helps.IsModelSubstituted`, the same comparison used for
model-substitution warnings. That comparison ignores case and whitespace,
parenthesized suffixes, provider prefixes, and `-latest`; it accepts a base model
paired with a date snapshot or three-digit numeric version. Distinct models
remain mismatches. Two distinct pinned dates remain mismatches as well.

Responses without a reported model pass validation. A reported mismatch returns
HTTP 502 with error code `response_model_mismatch`; a later matching event cannot
erase an earlier mismatch. Validation applies to non-streaming, SSE, and
WebSocket generation, including selected credential retries, fallback paths,
and Home-dispatched requests. Token-count operations keep their existing behavior.

## Streaming and Usage

Selected streaming requests retain all output until upstream EOF, so a model
reported only in a terminal event can still reject the entire response. This
increases time to the first client-visible token. Successful validation preserves
chunk order, response headers, and incomplete-response markers. WebSocket response
observer callbacks are released only after successful validation.

Each validated stream can buffer up to 64 MiB of output payload. Exceeding this
limit rejects the request with 502; cancellation stops upstream and drains pending
chunks. Internal consumption does not wait for downstream delivery acknowledgment.

Usage records are held until validation resolves. Rejected model substitutions
are recorded as failures with code `response_model_mismatch`, status 502, the
selected model, the reported model, and the upstream token counts. Original
upstream failures retain their classification. Local model mismatches stop
credential retries and do not cool credentials; configured upstream-error actions
cannot override the validation decision. Home uses the existing request-scoped
result contract; no Home protocol or data-schema extension is required.
