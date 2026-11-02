# Response Model Validation UI

## Summary

The management center exposes response-model validation in its network
configuration section. Rules select a provider key, an optional configuration
label, and resolved upstream model IDs. The editor supports adding and deleting
rules, displays matching and streaming behavior, and saves through the v8 API.

## Implementation

- Integrate rule drafts with loading, dirty tracking, validation, search and saving.
- Preserve rule identity, unknown fields and YAML comments during list edits.
- Reject concurrent list replacements through the existing configuration workflow.
- Write an explicit empty list when all rules are removed.
- Supply all seven supported locale translations and update both frontend READMEs.

## Verification

- Production build and focused ESLint checks passed.
- Two key regression tests passed: rule serialization/clearing and draft recovery/conflicts.
- Chrome checks with a simulated v8 backend passed for editing, required fields,
  wire payloads, clearing and desktop/mobile layouts. Screenshots were inspected.
- The UI is included in frontend merge commit
  [`5e918c9`](https://github.com/xzhao4545/Cli-Proxy-API-Management-Center-Ehance/commit/5e918c9e879e38398bb88bbd6f63314041f48f97),
  which integrates upstream `4e4aeff` and retains Enhance features.

Current contract: [Response Model Validation](../current/features/response-model-validation.md).
