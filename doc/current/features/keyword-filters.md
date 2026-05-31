# Keyword Filters

Keyword filters detect configured text in upstream response chunks. A match converts the response into a retryable provider failure so the conductor can mark the provider result as failed and continue fallback selection when possible.

## Rule Shape

Rules are configured through `keyword-filters`:

```yaml
keyword-filters:
  - keyword: "insufficient credits"
    match-mode: "anywhere"
    enabled: true
    case-sensitive: false
```

Fields:

- `keyword`: text to match.
- `match-mode`: `anywhere`, `start`, `end`, or `exact`; empty values behave as `anywhere`.
- `enabled`: inactive rules are skipped.
- `case-sensitive`: when false, matching uses case-insensitive text comparison.

## Payload Extraction

`internal/fork/keywordfilter` extracts text from common streaming payload formats:

- SSE `data:` frames, including JSON payloads nested after the `data:` prefix
- OpenAI-style `choices[].delta.content`
- OpenAI-style `choices[].message.content`
- OpenAI-style `choices[].text`
- OpenAI Responses / Codex events: `response.output_text.delta`, `response.output_text.done`, `response.content_part.*`, `response.output_item.*`, and `response.completed`
- Claude-style `content_block_delta` and `content_block_start`
- Gemini-style `candidates[].content.parts[].text`

SSE control-only frames such as `event:`, `id:`, and `retry:` are ignored. OpenAI Responses metadata events, Anthropic stream metadata events, and Gemini candidate frames that do not expose response text are ignored. If another JSON payload does not expose known text fields, or the chunk is not JSON, the raw chunk bytes are treated as text. Each payload is parsed once per check, then the extracted text is evaluated against the active rules in configured order. Stream checks keep bounded extracted text context across chunks so `start`, `end`, and `exact` modes use response-text boundaries even when upstream splits a sentence across multiple SSE frames.

## Runtime Behavior

The SDK auth conductor snapshots the active keyword filter rules once per stream attempt and checks them in two places:

- buffered bootstrap chunks before a stream is handed to callers
- forwarded stream chunks before they leave the wrapped stream result

During bootstrap, metadata-only chunks are buffered until the first chunk with extracted response text, a keyword match, an upstream error, stream close, or the bounded bootstrap buffer limit. This keeps OpenAI Responses and Anthropic metadata frames from committing HTTP streaming headers before the first real response text can be filtered. Buffered chunks that pass the bootstrap check are forwarded without a second keyword scan. Later chunks continue to be scanned as they are forwarded, using the same accumulated extracted response text for boundary-sensitive match modes.

On match, the conductor produces the error message:

```text
keyword filter matched: response contains "<matched text>" (keyword: "<keyword>")
```

The matched text is the original response text or a bounded excerpt around the matched keyword for large chunks. Matches are marked with code `keyword_filtered`, `Retryable: true`, and an internal HTTP status of `429`, so the current auth/model enters the existing quota cooldown path before fallback selection continues. If more candidate models or credentials are available, the conductor can continue to the next candidate before the downstream HTTP response is committed. Stream-time matches are recorded as failed usage with the `keyword_filtered` code and the same message body. If the stream is filtered before the upstream emits token usage, the request still produces a zero-token failed usage record so management statistics show the failure.

OpenAI Chat Completions streams return the normal OpenAI-compatible error body. OpenAI Responses streams and Codex websocket sessions emit a Responses-native `response.failed` event and keep the generic `error` event for compatibility. Claude-compatible streams emit a Claude SSE `event: error` response with the keyword filter message in the error body.

## Management

Rules are managed through `/v0/management/keyword-filters` with `GET`, `PUT`, `PATCH`, and `DELETE`. Management writes persist through the normal config persistence path.
