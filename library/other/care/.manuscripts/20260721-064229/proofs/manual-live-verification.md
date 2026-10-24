# Manual live verification (2026-10-23)

Live verification was executed against the real `care.com` API (`https://www.care.com/api/graphql` and Stream Chat `https://chat.stream-io-api.com`) using an authenticated browser session (`CARE_SESSION_COOKIE`) and verified alongside the automated Phase 5 acceptance run (`phase5-acceptance.json`, 222/222 matrix checks passed).

All mutating operations (`favorite`, `unfavorite`, `outreach send`, `messages reply`) were exercised exclusively in dry-run mode so no live account state or caregiver threads were modified.

## Live Command Verification & Counts

- **`care-pp-cli job list --json`**
  - Returned **3** job posts on the authenticated seeker account (`serviceType: "CHILD_CARE"`, with applicant counts `19`, `32`, and `1`).
- **`care-pp-cli find --job "Summer Sitter" --zip 90210 --limit 3 --json`**
  - Resolved the seeker job context and returned **3** ranked caregivers in `Beverly Hills, CA` (e.g., `8y exp, 15 reviews, 5.0 rating, has_care_check: true`; `10y exp, $31/hr, 5 reviews, 5.0 rating, has_care_check: true`; `5y exp, $35/hr, has_care_check: true`).
- **`care-pp-cli recommend --zip 90210 --target-rate 22 --json`**
  - Scored and ranked **10** caregivers near `90210` with weighted `0-100` `fit_score` values and human-readable `reasons` (`reviews`, `stars`, `exp`, `background-checked`, `hired Nx`, `hourly_rate`).
- **`care-pp-cli caregiver <caregiver-uuid> --json`**
  - Fetched a real caregiver profile via `GetCaregiver` (`care-pp-cli caregiver`), returning full structured detail:
    ```json
    {
      "age_groups": ["newborns", "toddlers", "preschoolers", "school-age kids", "teens"],
      "credentials": ["a non-smoker", "an early-childhood-education background", "experience with special-needs care"],
      "education": "Orange Coast College",
      "has_care_check": true,
      "hourly_rate": 30,
      "id": "<redacted-caregiver-uuid>",
      "max_children": 4,
      "name": "Nancie A.",
      "profile_url": "https://www.care.com/app/vip/<redacted-caregiver-uuid>",
      "years_experience": 10
    }
    ```
- **`care-pp-cli outreach draft <caregiver-uuid> --from "The Fixture family" --json`**
  - Fetched the live caregiver profile via `GetCaregiver` (`care-pp-cli outreach draft`) and composed a personalized ASCII outreach draft referencing `10 years of childcare experience`, age groups (`newborns, toddlers, preschoolers, school-age kids, and teens`), credentials (`a non-smoker, an early-childhood-education background`), education (`Orange Coast College`), and `a verified background check`, with `"sent": false`.
- **`care-pp-cli sync list --zip 90210 --min-exp 5 --json`**
  - Queried the local SQLite mirror (`care.db`) and returned synced caregiver records matching the filter criteria.
- **`care-pp-cli messages --limit 3 --json` & `care-pp-cli messages read "<caregiver>" --limit 2 --json`**
  - Extracted the live `streamApiKey` and `streamToken` from `https://www.care.com/app/messages`, listed **3** active conversations (`messaging:!members-...`), and fetched **2** messages from the latest thread with `"has_more": true` and `"next_before"` pagination cursor.
- **Dry-run write safety (`favorite`, `unfavorite`, `outreach send`, `messages reply`)**
  - Verified `--dry-run --json` on all write/mutating commands; each returned `"dry_run": true` and a non-empty `"action"` description without performing any network mutation.
