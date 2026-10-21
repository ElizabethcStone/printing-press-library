# Phase 4.9 document correctness audit

Result: **PASS — README/SKILL/AGENTS correctness verified after five findings were fixed.**

Reviewed the three documents against the CLI help, command implementations, current research.json `novel_features_built` and the approved absorb manifest. This pass changed documentation only; no laboratory operations or additional test suites ran.

| Finding | Severity | Fix and final location | Status |
| --- | --- | --- | --- |
| Executable examples contained command, type, ID and playbook placeholders | Error | Concrete discovery/help/query examples and illustrative form ID 42; README.md:62,307; SKILL.md:127,281,503,520–531,665–668; AGENTS.md:52 | FIXED |
| Integrated teach example omitted required resource type | Error | Added `--resource-type forms`, matching teach validation and actual help; SKILL.md:520 | FIXED |
| README named nonexistent auth-status command | Error | Corrected to `crocoblock-pp-cli auth status`; README.md:524 | FIXED |
| Missing-binary route selected unpublished catalog installation | Error | Explicit local-build selection before the canonical prerequisites and in Direct Use; SKILL.md:38,661. Canonical installation blocks preserved exactly. | FIXED |
| Header override prose suggested a public per-command option | Warning | Documented static config headers and absence of a curated per-invocation override flag; README.md:511 | FIXED |

The illustrative IDs and file names require the operator's verified inputs. Placeholders retained in command-signature tables and descriptive response schemas describe argument shapes rather than executable recipes. Runtime authentication uses the WordPress site credentials; local analyses use captured evidence. Current novel descriptions match the six built analyses, including reservation versions and recurrence limits, limited filter-provider support, conservative relation endpoint handling, and the non-atomic field-restore check. The local/publication distinction remains explicit.

Validation:

- `verify-skill --dir working/crocoblock-pp-cli --json`: **PASS**, exit 0, 49 recipes checked, all six checks ran, zero findings. Includes canonical-sections validation.
- Seven help checks: **PASS**, exit 0 for engine configuration, forms, discover, auth status, teach, teach-playbook and which. The teach help declares `--resource-type`.
- Raw reports: `phase-4.9-verify-skill.json` and `phase-4.9-help-checks.json`.

No open findings or accepted warnings from this audit. The parent owns the phase receipt and subsequent review gate.
