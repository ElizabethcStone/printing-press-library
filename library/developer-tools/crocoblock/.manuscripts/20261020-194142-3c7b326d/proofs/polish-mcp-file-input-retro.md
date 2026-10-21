# Local Printing Press retro: required file inputs in MCP mirrors

Status: local mitigation applied; no publication or external issue created.

The generated walker intentionally suppresses `mcp:read-flags` and `mcp:write-flags` in the schema and rejects those names at invocation. It nevertheless advertises commands whose only usable path requires one of those inputs. Relevant sources: `internal/mcp/cobratree/classify.go:49`, `typemap.go:309`, `walker.go:41` and `shellout.go:62`.

Crocoblock had five domain workflows affected (schema diff, CCT reconcile, reservation overlaps, field update and field restore), plus framework import without an inline payload alternative. The local curated layer now adds `mcp:hidden` while retaining CLI execution and declares field snapshot output with `mcp:write-flags=snapshot`. No generated cobratree file changed. The future generator fix should identify required unavailable file inputs and suppress such tools, or provide a separately reviewed safe inline contract. It must retain blocked-path and raw-flag protections.

Regression evidence: ten remaining required-ID cases, six absent-MCP/present-CLI cases, configured-store tools retained, built final MCP lists exactly 75 tools. Patch durability is recorded in `.printing-press-patches/mcp-local-file-workflow-exclusions.json`.
