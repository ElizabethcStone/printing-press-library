1. **README.md:129 — Medium.** `doctor --dry-run` skips the live checks, so it does not verify authentication or connectivity as the comment promises. **Fix:** use `airtable-pp-cli doctor` for verification, or describe the dry run as a preview.

2. **README.md:431–434 — Medium.** `bases get` and `bases search` are listed as covered freshness paths, but neither is an available `bases` command. **Fix:** list only command paths exposed by the binary and actually covered by the freshness hook.

3. **SKILL.md:172–175 — Medium.** The same nonexistent freshness paths (`bases get`, `bases search`) appear here. **Fix:** align this list with the commands exposed by the binary and the hook’s real coverage.

4. **README.md:422, 537–548 — Medium.** The exit-code lists omit code `1`, which the CLI returns for unclassified errors. **Fix:** document code `1` as the generic error fallback, or label the lists as covering only classified errors.

5. **README.md:460 — Low.** `auth-status` reads like a command, but the binary exposes `auth status`. **Fix:** change it to `auth status`.

6. **AGENTS.md:17–18, 24, 30–31; SKILL.md:418, 433–445, 577–579 — Low.** Executable command templates retain placeholder literals such as `<command>`, `<resource>`, `<id>`, and `<type>`. **Fix:** mark these as templates outside runnable code examples, or replace them with concrete values.

Read-only audit; no files modified and no tests run. I did not validate external installer or third-party product claims; those may drift independently of the shipped CLI.