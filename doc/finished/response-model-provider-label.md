# Response Model Provider Labels

## Summary

Response-model validation rules accept an optional `label` alongside `provider`
to select a specific provider configuration. Credentials with other labels do
not match; omitting the label covers the whole provider key.

## Implementation

- Match the optional label against the selected credential's runtime label.
- Preserve configured labels in OpenAI-compatible and Vertex-compatible credentials,
  making those labels available to validation rules and raw usage records.
- Keep provider routing names independent of display labels.

## Verification

- Updated the existing scope/reload test to check isolation between provider labels.
- Added one key test covering runtime label propagation and retained routing names.
- Both key tests and the required server build pass.

Configuration: [Response Model Validation](../current/features/response-model-validation.md).
