## Findings

1. **Trigger phrases — PASS.** The listed phrases map to shipped CLI capabilities.

2. **Verified-set alignment — PASS.** “Unique Capabilities” contains exactly the four commands in `novel_features_built`; it omits `records list/get --expand-links`.

3. **Novel-feature descriptions — warning, [SKILL.md:80](<runstate>/headaideveloper-ca938232/runs/20261020-032053-d846692a/working/airtable-pp-cli/SKILL.md:80).** The skill says schema differences are measured since the command last ran, while CLI help says since the last sync; change the wording to “since the last sync.”

4. **Stub/gated disclosure — PASS.** The search capability identifies synced local data as its scope; the other listed features show no stub or gate in the reviewed help.

5. **Auth narrative — error, [SKILL.md:225](<runstate>/headaideveloper-ca938232/runs/20261020-032053-d846692a/working/airtable-pp-cli/SKILL.md:225).** The skill describes OAuth as supported, but this CLI’s auth commands expose API-token setup, storage, and status; remove the OAuth claim or clarify that it applies to an external integration.

6. **Recipe output claims — warning, [SKILL.md:197](<runstate>/headaideveloper-ca938232/runs/20261020-032053-d846692a/working/airtable-pp-cli/SKILL.md:197).** `--expand-links` resolves names from the local mirror, so state that the table must be synced first; also correct the recipe’s schema-diff timing at [SKILL.md:221](<runstate>/headaideveloper-ca938232/runs/20261020-032053-d846692a/working/airtable-pp-cli/SKILL.md:221) to “since the last sync.”

7. **Marketing-copy smell — warning, [SKILL.md:3](<runstate>/headaideveloper-ca938232/runs/20261020-032053-d846692a/working/airtable-pp-cli/SKILL.md:3).** “Every Airtable REST API feature” and “nothing else offers” read as unsupported promotional claims; replace them with concrete supported capabilities.

**Skipped:** I did not run tests or modify files. **Risk:** Review was limited to the specified sources and CLI help.