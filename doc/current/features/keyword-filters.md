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
- `case-sensitive`: when false, matching lowercases both text and keyword.

## Payload Extraction

`internal/fork/keywordfilter` extracts text from common streaming payload formats:

- OpenAI-style `choices[].delta.content`
- OpenAI-style `choices[].message.content`
- OpenAI-style `choices[].text`
- Claude-style `content_block_delta` and `content_block_start`
- Gemini-style `candidates[].content.parts[].text`

If a JSON payload does not expose known text fields, or the chunk is not JSON, the raw chunk bytes are treated as text.

## Runtime Behavior

The SDK auth conductor checks keyword filters in two places:

- buffered bootstrap chunks before a stream is handed to callers
- forwarded stream chunks before they leave the wrapped stream result

On match, the conductor produces the error message:

```text
keyword filter matched: response contains "<matched text>" (keyword: "<keyword>")
```

Bootstrap matches are marked with code `keyword_filtered`, `Retryable: true`, and recorded as failed provider results. If more candidate models or credentials are available, the conductor can continue to the next candidate.

## Management

Rules are managed through `/v0/management/keyword-filters` with `GET`, `PUT`, `PATCH`, and `DELETE`. Management writes persist through the normal config persistence path.
