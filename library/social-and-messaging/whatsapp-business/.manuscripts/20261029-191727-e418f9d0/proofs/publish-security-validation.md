# Publish-time security validation

The publish preflight found an un-tidy module and reachable standard-library findings under the previously selected Go1.27.1 toolchain. The module now selects Go1.27.3, go mod tidy passes, and scoped govulncheck reports no reachable vulnerabilities. The module minimum remains Go1.26.10.

Official references: https://go.dev/doc/devel/release#go1.27.3 and https://pkg.go.dev/vuln/GO-2026-6809.

The CLI, MCP peer and MCPB bundle were rebuilt with the corrected toolchain. The publish-time full authenticated matrix passed165 checks,0 failed,140 explicitly skipped/unverified. Only reads and write previews were authorized and executed. The runner refreshed source-bound acceptance in embedded and archived copies; raw response transcript stayed in a private temporary directory and was removed.

Public category remains social-and-messaging. Public package rewrites the local module path to the canonical library path. No runtime release version was stamped.
