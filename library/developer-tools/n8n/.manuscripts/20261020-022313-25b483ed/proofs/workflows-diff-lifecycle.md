# workflows diff — live lifecycle proof

Sandbox: disposable local n8n 2.44.4 container (pp-n8n-test), PRIVATE TEST workflows only. No production instance touched.

## Saved version -> current (expect exit 0)
```
$ n8n-pp-cli workflows diff CW7fpwFVp2DYBA3P --version be308c02-68bc-49ce-8f19-4faa1110651b --agent
{
  "meta": {
    "source": "live"
  },
  "results": {
    "base": {
      "label": "version:be308c02-68bc-49ce-8f19-4faa1110651b",
      "version_id": "be308c02-68bc-49ce-8f19-4faa1110651b"
    },
    "changed": false,
    "diff": {
      "active_changed": false,
      "credential_changes": [],
      "disabled_changes": [],
      "edges_added": [],
      "edges_removed": [],
      "nodes_added": [],
      "nodes_removed": [],
      "nodes_renamed": [],
      "param_changes": [],
      "settings_changes": [],
      "type_changes": []
    },
    "target": {
      "label": "current"
    },
    "workflow_id": "CW7fpwFVp2DYBA3P"
  }
}
exit: 0
```
## Workflow -> another workflow on the same instance (expect exit 0)
```
$ n8n-pp-cli workflows diff CW7fpwFVp2DYBA3P --against-id XYCfsq2RGHjUOTZH --agent
{
  "meta": {
    "source": "live"
  },
  "results": {
    "base": {
      "label": "current"
    },
    "changed": true,
    "diff": {
      "active_changed": false,
      "credential_changes": [],
      "disabled_changes": [],
      "edges_added": [
        {
          "from": "Webhook",
          "kind": "main",
          "output": 0,
          "to": "Boom"
        }
      ],
      "edges_removed": [
        {
          "from": "Webhook",
          "kind": "main",
          "output": 0,
          "to": "Call Sub"
        }
      ],
      "name_change": {
        "after": "PRIVATE TEST Webhook Fails",
        "before": "PRIVATE TEST Webhook OK",
        "node": "",
        "path": "name"
 
exit: 0
```
## Unknown id (expect not-found exit)
```
$ n8n-pp-cli workflows diff doesnotexist000 --against-id XYCfsq2RGHjUOTZH --agent
Error: doesnotexist000 not found on the base side (http://localhost:5678/api/v1); check the id and which instance it lives on
exit: 3
```
## Cleanup
Read-only command; no state created.
