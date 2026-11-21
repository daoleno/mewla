# Fetches an artifact of the running workflow while it is still being built (release-launch.sh, release-flows.sh): `source` it.
# fetch_artifact NAME BUILD_JOB DIR waits up to 40 min for the artifact NAME, ends the wait when job BUILD_JOB ends without it,
# and unpacks it into DIR. Needs gh, GH_TOKEN, GITHUB_REPOSITORY, GITHUB_RUN_ID and a `fail MESSAGE` and `log MESSAGE` of the caller.
fetch_artifact() {
  local name="$1" job="$2" dir="$3" id="" state
  # The artifacts API, not `gh run download`: this run is still going.
  local api="repos/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID"
  log "waiting for $name"
  for _ in $(seq 1 240); do
    id="$(gh api "$api/artifacts?name=$name" --jq '.artifacts[0].id // empty' 2>&1)" || { log "artifacts API: $id"; id=""; }
    [[ -z "$id" ]] || break
    state="$(gh api "$api/jobs?per_page=100" --jq ".jobs[] | select(.name == \"$job\") | \"\(.status) \(.conclusion)\"" 2>&1)" || state=""
    [[ "$state" != completed* || "$state" == "completed success" ]] || fail "$job ended without $name ($state)"
    sleep 10
  done
  [[ -n "$id" ]] || fail "no $name after 40 min"
  mkdir -p "$dir"
  timeout 600 gh api "repos/$GITHUB_REPOSITORY/actions/artifacts/$id/zip" > "$dir/artifact.zip" || fail "downloading artifact $id failed"
  (cd "$dir" && unzip -q artifact.zip && rm artifact.zip) || fail "artifact $id is not a zip"
}
