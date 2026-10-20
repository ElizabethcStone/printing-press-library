#!/bin/zsh
# Records every command verbatim with its exit code and a trimmed output sample.
cd ${0:a:h}; . ./env.sh; export PATH="$PWD:$PATH"
LOG=roundtrip.log; : > $LOG
run(){ print -r -- "\$ $*" >> $LOG; out=$("$@" 2>&1); rc=$?; print -r -- "exit=$rc" >> $LOG; print -r -- "${out[1,700]}" | sed 's/^/    /' >> $LOG; print >> $LOG; }
wpc(){ docker compose -f wp/compose.yml exec -T cli wp "$@"; }
P=http://localhost:8889
run rankmath-pp-cli sync --json
run rankmath-pp-cli posts --json --select id,slug
# baseline: no Rank Math meta on post 4 yet
run wpc post meta get 4 rank_math_title
# meta write -> read back
run rankmath-pp-cli meta update --object-id 4 --object-type post --meta '{"rank_math_title":"PRIVATE TEST SEO title v2","rank_math_description":"PRIVATE TEST meta description"}' --json
run rankmath-pp-cli head --url $P/private-test-post-1/ --json
run wpc post meta get 4 rank_math_title
# schema write -> read back
run rankmath-pp-cli schema update --object-id 4 --object-type post --schemas '{"new-9001":{"@type":"Article","headline":"PRIVATE TEST headline","metadata":{"title":"Article","type":"template","isPrimary":true}}}' --json
run wpc post meta list 4 --keys=rank_math_schema_Article --format=json
run rankmath-pp-cli head --url $P/private-test-post-1/ --json
# settings write: toggle the redirections module (creates its tables)
run rankmath-pp-cli settings save-module --module redirections --state off --json
run rankmath-pp-cli settings save-module --module redirections --state on --json
# redirection write -> verify 301 -> remove -> verify 200
run rankmath-pp-cli redirections --object-id 5 --redirection-sources $P/private-test-post-2/ --redirection-url $P/private-test-page/ --redirection-type 301 --has-redirect true --json
run curl -s -o /dev/null -w '%{http_code} %{redirect_url}' $P/private-test-post-2/
run wpc db query "SELECT id,url_to,header_code,status FROM wp_rank_math_redirections"
RID=$(docker compose -f wp/compose.yml exec -T cli wp db query "SELECT id FROM wp_rank_math_redirections ORDER BY id DESC LIMIT 1" --skip-column-names)
run rankmath-pp-cli redirections --object-id 5 --redirection-id $RID --has-redirect false --json
run wpc db query "SELECT id,status FROM wp_rank_math_redirections"
run curl -s -o /dev/null -w '%{http_code} %{redirect_url}' $P/private-test-post-2/
# Pro: Link Genius keyword map lifecycle
run rankmath-pp-cli link-genius create-keyword-maps --name "PRIVATE TEST map" --target-url $P/private-test-page/ --json
KM=$(rankmath-pp-cli link-genius list-keyword-maps --json 2>/dev/null | python3 -c "import json,sys;print(json.load(sys.stdin)['results']['items'][0]['id'])")
run rankmath-pp-cli link-genius list-keyword-maps --json
run rankmath-pp-cli link-genius get-keyword-maps $KM --json
run rankmath-pp-cli link-genius keyword-maps-variations $KM --variation "PRIVATE TEST variation" --json
run rankmath-pp-cli link-genius get-keyword-maps-variations $KM --json
run rankmath-pp-cli link-genius keyword-maps-preview $KM --json
run rankmath-pp-cli link-genius keyword-maps-preview-results --keyword-map-id $KM --json
run rankmath-pp-cli link-genius delete-keyword-maps $KM --json
run rankmath-pp-cli link-genius get-keyword-maps $KM --json
# reindex preview -> confirm -> indexing log read back
run rankmath-pp-cli reindex --since 7d --json
run rankmath-pp-cli indexing get-log --json
# coverage / opportunities authenticated reads
run rankmath-pp-cli coverage --json
run rankmath-pp-cli opportunities --json
