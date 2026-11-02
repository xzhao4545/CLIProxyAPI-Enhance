# Response Model Validation

`response-model-validation` enables upstream response validation for selected
provider and resolved-model pairs. It is an optional root section in both
configuration layouts and supports hot reload.

```yaml
response-model-validation:
  - provider: codex
    models: [gpt-5.4, gpt-5.4-mini]
  - provider: my-openai-compatible-provider
    models: [gpt-4o]
```

`provider` is the executor's provider key: for example `codex`, `claude`,
`gemini`, or an OpenAI-compatible provider's configured name. Provider labels
are display names and do not select rules. `models` lists resolved model IDs
after alias and model-pool selection. Rule selection ignores case, surrounding
whitespace, and the request's parenthesized thinking suffix. An empty rule
list disables validation.

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
