# PR review regressions

All three first-round Greptile findings were valid and fixed before a new push.

- Raw JSONL import is hidden and always refuses execution; the multiplexed resource is removed from generic writable resources once during process initialization. A mock transport canary proves that payload confirm=true, --yes, agent and dry-run variants make zero requests.
- Local inspection output explicitly retains its observed evidence keys during compact rendering. A three-row regression with two removals and one status change proves rare before and after values survive --agent, including an explicit user projection.
- sync accepts a validated JSON arguments object for typed read filters. A loopback MCP fixture proves an array with two status values reaches the selected read operation intact. Non-objects, trailing JSON objects and mixed argument sources are refused; sync still rejects mutating tools.

Focused race tests, full package tests and go vet pass. Native remote tests remain read-only; no real import or outbound-message write was attempted. The new Printing Press4.33.9 generated the canonical SKILL install block verbatim.
