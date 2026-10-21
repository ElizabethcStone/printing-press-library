# executions wait — live lifecycle proof

Sandbox: disposable local n8n 2.44.4 container (pp-n8n-test), PRIVATE TEST workflows only. No production instance touched.

## Trigger a succeeding run, then wait on it (expect exit 0)
```
$ curl -s -o /dev/null -w %{http_code} http://localhost:5678/webhook/pp-test-ok
200
exit: 0
```
```
$ n8n-pp-cli executions wait --workflow CW7fpwFVp2DYBA3P --after 1m --max-wait 30s --agent
{
  "meta": {
    "source": "live"
  },
  "results": {
    "duration_ms": 69,
    "error_message": "",
    "execution_id": "11",
    "failed_node": "",
    "finished": true,
    "found": true,
    "started_at": "2026-10-20T16:24:18.685Z",
    "status": "success",
    "stopped_at": "2026-10-20T16:24:18.754Z",
    "timed_out": false,
    "waited_ms": 21,
    "workflow_id": "CW7fpwFVp2DYBA3P"
  }
}
exit: 0
```
## Trigger a failing run, then wait on it (expect exit 1 with failing node)
```
$ curl -s -o /dev/null -w %{http_code} http://localhost:5678/webhook/pp-test-fail
200
exit: 0
```
```
$ n8n-pp-cli executions wait --workflow XYCfsq2RGHjUOTZH --after 1m --max-wait 30s --agent
{
  "meta": {
    "source": "live"
  },
  "results": {
    "duration_ms": 13,
    "error_message": "PRIVATE TEST deliberate failure [line 1]",
    "execution_id": "16",
    "failed_node": "Boom",
    "finished": false,
    "found": true,
    "started_at": "2026-10-20T16:24:42.579Z",
    "status": "error",
    "stopped_at": "2026-10-20T16:24:42.592Z",
    "timed_out": false,
    "waited_ms": 524,
    "workflow_id": "XYCfsq2RGHjUOTZH"
  }
}
Error: execution 16 ended with status error
exit: 1
```
## Verification read
```
$ n8n-pp-cli executions list --workflow-id XYCfsq2RGHjUOTZH --limit 1 --agent --select id,status
{"event":"truncated","hint":"pass --all to fetch every page"}
{
  "meta": {
    "freshness": {
      "decision": "fresh",
      "ran": false,
      "reason": "fresh",
      "resources": [
        "executions"
      ],
      "elapsed_ms": 1,
      "source": "auto"
    },
    "source": "live"
  },
  "results": [
    {
      "id": "13",
      "status": "error"
    }
  ]
}
exit: 0
```
## Cleanup
Executions created here live only in the throwaway container; nothing to clean on any shared instance.
