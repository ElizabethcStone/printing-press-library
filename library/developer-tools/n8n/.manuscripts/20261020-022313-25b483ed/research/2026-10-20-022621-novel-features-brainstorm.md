## Customer model
- Dana — self-hosted n8n operator (dev/staging/prod). Today: Executions page filtered to Error, click each row for failing node; can't group; can't answer "which active workflows haven't succeeded in a week". Ritual: daily triage, weekly hygiene. Frustration: flat paginated error list, failing node hidden behind includeData.
- Marco — automation consultant, 100+ workflows across client instances. Today: tab per instance + git repo of exported JSON; greps JSON for credential/webhook usage; eyeballs dev->prod. Ritual: change requests (find usages), promotion, rotation/handoff cleanup. Frustration: no "find usages" in UI.
- AI coding agent — edit loop get/patch/update/run/check. Frustration: no blocking wait with a branchable result; hand-rolled poll loops.

## Candidates (pre-cut)
C1 executions failures (a,e) | C2 workflows uses (a,b,c,e) | C3 workflows deps (b,e) | C4 workflows stale (a,c,e) | C5 credentials audit (c,e) | C6 workflows node-inventory (e) | C7 executions wait (a,e) | C8 workflows diff (a,b,e) | C9 variables diff (e) | C10 executions rate (e) | C11 workflows bulk (e) | C12 executions export (e) | C13 doctor compare (e) | C14 workflows webhooks (b) | C15 executions slow (c) | C16 workflows changed (c)

## Survivors and kills
### Survivors
1 executions failures 10/10 hand-code (Dana) | 2 workflows uses 10/10 hand-code (Marco, agent) | 3 workflows stale 9/10 hand-code (Dana) | 4 credentials audit 8/10 hand-code (Marco) | 5 executions wait 9/10 hand-code (agent) | 6 workflows diff 9/10 hand-code (Marco, agent)

### Killed candidates
| Feature | Kill reason | Closest surviving sibling |
|---|---|---|
| workflows deps | uses --subworkflow covers direct callers weekly; transitive chains monthly | workflows uses |
| workflows node-inventory | framework analytics --type workflow_nodes --group-by type already answers it | credentials audit |
| variables diff | variables few and rarely change; monthly | workflows diff |
| executions rate | failure counts already in executions failures | executions failures |
| workflows bulk | thin loop over generated activate/deactivate | workflows stale |
| executions export | sync --resources executions --since 7d + --json covers it | executions wait |
| doctor compare | rare; reserved root name | workflows diff |
| workflows webhooks | weekly need served by uses --webhook | workflows uses |
| executions slow | speculative, no persona asks for duration | executions failures |
| workflows changed | thin updatedAt filter; value lives in diff | workflows diff |

Verified post-hoc against spec: GET /credentials, GET /workflows/{id}/history, GET /workflows/{id}/versions/{vid} exist.
