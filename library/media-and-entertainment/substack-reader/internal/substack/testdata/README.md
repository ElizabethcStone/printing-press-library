# Test fixtures

`real/` holds `body_html` cut from posts of the 2026-10-22 UX Mentor Diaries audit
(three paid posts, one free post, one free page). It is **not committed**: the paid
text is not ours to republish, and the free text belongs to its author. The tests
that need it skip when the folder is empty.

Refill it from the audit folder (`reads/` is kept out of git too):

    python3 -I - <<'PY'
    import json, os
    A = os.path.expanduser("~/Cowork/projects/personal/substack-cli/audits/uxmentor-2026-10-22/")
    S = [("01-free", "reads/posts/before-monday-hubspot-cut-managers.json"),
         ("02-paid", "reads/posts/the-silent-career-killer-21.json"),
         ("03-paid-template", "reads/posts/how-to-turn-your-leadership-portfolio.json"),
         ("04-paid-pdf-attachments", "reads/posts/leadership-launchpad.json"),
         ("05-page", "reads/pages/frameworks-and-resources.json")]
    for n, p in S:
        open("internal/substack/testdata/real/%s.html" % n, "w").write(json.load(open(A + p))["body_html"])
    PY

The committed unit tests use short snippets that copy the markup Substack sends
(class names, attribute layout) but carry no article text.
