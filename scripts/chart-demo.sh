#!/usr/bin/env bash
# Walk through query rendering: tables, bar charts, and pivots.
# Read-only queries against a sales pipeline demo model.
#
#   OMNI_PROFILE=my-profile MODEL_ID=... scripts/chart-demo.sh
#   PAUSE=1 ...                      # wait for Enter between examples
#   VIEW=... TOPIC=...               # if your model names them differently
set -uo pipefail

cd "$(dirname "$0")/.."
: "${OMNI_PROFILE:?set OMNI_PROFILE to the omni config profile to query with}"
: "${MODEL_ID:?set MODEL_ID to a model with the sales pipeline demo topic}"
PROFILE=$OMNI_PROFILE
M=$MODEL_ID
D=${VIEW:-apps_demos_sales_pipeline__deals}
T=${TOPIC:-sales_pipeline}

[[ -x bin/omni ]] || make build >/dev/null

bold=$'\e[1m' dim=$'\e[2m' reset=$'\e[0m'

# q FIELDS [EXTRA_JSON] -> a query/run body
q() {
  local fields="" f
  for f in $1; do fields+="${fields:+,}\"$D.$f\""; done
  printf '{"query":{"modelId":"%s","table":"%s","topic":"%s","fields":[%s],"limit":100%s}}' \
    "$M" "$D" "$T" "$fields" "${2:-}"
}
by_region=',"sorts":[{"column_name":"'$D'.region","sort_descending":false}]'
pivot_stage="$by_region"',"pivots":["'$D'.stage"]'

step() {
  echo
  echo "${bold}━━ $1${reset}"
  shift
  echo "${dim}\$ omni $*${reset}"
}
pause() { [[ -n ${PAUSE:-} ]] && read -r -p "${dim}(enter)${reset}" _; return 0; }
run() { bin/omni --profile "$PROFILE" "$@"; local s=$?; [[ $s -ne 0 ]] && echo "${dim}exit $s${reset}"; pause; }

echo "${bold}omni query rendering demo${reset}  ${dim}profile=$PROFILE, terminal width ${COLUMNS:-$(tput cols)}${reset}"

# ── One dimension, several measures ──────────────────────────────────────────
F1="region count total_amount won_amount win_rate"
step "1 dim × 4 measures: table" query run --format human
run query run --format human --body "$(q "$F1")"

step "1 dim × 4 measures: chart (every measure, each on its own scale)" query run --chart
run query run --chart --body "$(q "$F1")"

step "Two measures, picked by label and field" query run --chart --chart-value "Win rate",count
run query run --chart --chart-value "Win rate",count --body "$(q "$F1")"


# ── Two dimensions, two measures ─────────────────────────────────────────────
F2="region stage total_amount count"
step "2 dims × 2 measures: table" query run --format human
run query run --format human --body "$(q "$F2" "$by_region")"

step "2 dims × 2 measures: chart (both dimensions label each row)" query run --chart
run query run --chart --body "$(q "$F2" "$by_region")"

step "Row cap" query run --chart --chart-rows 5
run query run --chart --chart-rows 5 --body "$(q "$F2" "$by_region")"

# ── Pivots ───────────────────────────────────────────────────────────────────
F3="region stage total_amount"
step "Pivot (region × stage, 1 measure): table, as the workbook shows it" query run --format human
run query run --format human --body "$(q "$F3" "$pivot_stage")"

step "Pivot: chart (stages share one scale; widen the terminal for more columns)" query run --chart
run query run --chart --body "$(q "$F3" "$pivot_stage")"

step "Pivot with 2 measures: table" query run --format human
run query run --format human --body "$(q "$F2" "$pivot_stage")"

step "Pivot with 2 measures: chart, narrowed to count" query run --chart --chart-value count
run query run --chart --chart-value count --body "$(q "$F2" "$pivot_stage")"

step "Pivot + workbook link (compare with the Omni app)" query run --format human --workbook
run query run --format human --workbook --body "$(q "$F3" "$pivot_stage")"

# ── Pipes ────────────────────────────────────────────────────────────────────
step "Piped: still draws" "query run --chart | cat"
bin/omni --profile "$PROFILE" query run --chart --chart-value total_amount --body "$(q "$F1")" | cat
pause
