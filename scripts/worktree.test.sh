#!/usr/bin/env bash
# Exercises every `clean` verdict in a throwaway clone, with `gh` stubbed so no real PR or worktree is involved.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/worktree.sh"
tmp="$(mktemp -d)"
tmp="$(cd "$tmp" && pwd -P)"
sleeper=""
cleanup() {
  if [[ -n $sleeper ]]; then kill "$sleeper" 2>/dev/null || true; fi
  rm -rf "$tmp"
}
trap cleanup EXIT

git init --quiet --bare --initial-branch=main "$tmp/origin.git"
git clone --quiet "$tmp/origin.git" "$tmp/repo" 2>/dev/null
cd "$tmp/repo"
git config user.name test
git config user.email test@example.com
git commit --quiet --allow-empty -m base
git push --quiet origin HEAD:main

mkdir "$tmp/bin"
export PATH="$tmp/bin:$PATH"
pr_rows="$tmp/prs.tsv"
: >"$pr_rows"
printf '#!/bin/sh\ncat "%s"\n' "$pr_rows" >"$tmp/bin/gh"
chmod +x "$tmp/bin/gh"

task() { "$script" new "$1" "chore/$1-case" 2>/dev/null; }
commit_in() { git -C "$1" commit --quiet --allow-empty -m "$2"; }
pr() { printf '%s\t%s\tchore/%s-case\t%s\n' "$1" "$2" "$1" "$3" >>"$pr_rows"; }
head_of() { git -C "$1" rev-parse HEAD; }
pushed_head() { git -C "$1" push --quiet origin HEAD; head_of "$1"; }

merged="$(task 1)"; commit_in "$merged" work; pr 1 MERGED "$(pushed_head "$merged")"
open="$(task 2)"; commit_in "$open" work; pr 2 OPEN "$(pushed_head "$open")"
closed="$(task 3)"; commit_in "$closed" work; pr 3 CLOSED "$(pushed_head "$closed")"
no_pr="$(task 4)"; commit_in "$no_pr" work
dirty="$(task 5)"; commit_in "$dirty" work; pr 5 MERGED "$(pushed_head "$dirty")"; touch "$dirty/stray"
unpushed="$(task 6)"; commit_in "$unpushed" work; pr 6 MERGED "$(pushed_head "$unpushed")"; commit_in "$unpushed" more
brand_new="$(task 7)"
running="$(task 8)"; commit_in "$running" work; pr 8 MERGED "$(pushed_head "$running")"
(cd "$running" && exec sleep 300) &
sleeper=$!
just_left="$(task 9)"; commit_in "$just_left" work; pr 9 MERGED "$(pushed_head "$just_left")"

age_agent() { touch -t 202001010000 "$(git -C "$1" rev-parse --absolute-git-dir)"; }
agent() { git worktree add --quiet -b "worktree-agent-$1" ".claude/worktrees/agent-$1" origin/main; echo "$PWD/.claude/worktrees/agent-$1"; }
stale="$(agent stale)"; age_agent "$stale"
fresh="$(agent fresh)"
ahead="$(agent ahead)"; age_agent "$ahead"; commit_in "$ahead" work
pushed="$(agent pushed)"; age_agent "$pushed"; git -C "$pushed" push --quiet origin HEAD

outside="$tmp/outside"
git worktree add --quiet -b chore/12-case "$outside" origin/main
commit_in "$outside" work; pr 12 MERGED "$(pushed_head "$outside")"

failures=0
expect() {
  local got="missing"
  if grep -q "$2" <<<"$output"; then got="ok"; fi
  if [[ $got == ok ]]; then echo "ok    $1"; else echo "FAIL  $1: expected '$2'"; failures=$((failures + 1)); fi
}
exists() { [[ -d $1 ]]; }

before="$(git worktree list | wc -l)"
output="$("$script" clean)"
expect "dry run names the merged worktree" "would remove .*task-1 "
expect "dry run says nothing was removed" "Dry run"
if [[ $(git worktree list | wc -l) != "$before" ]]; then echo "FAIL  dry run removed something"; failures=$((failures + 1)); fi

output="$("$script" clean --yes --keep "$just_left")"
echo "$output" | sed 's/^/      /'
expect "primary kept" "keep .*repo: primary checkout"
expect "merged PR removed" "removed .*task-1 and branch chore/1-case: PR #1 merged"
expect "open PR kept" "task-2: PR #2 open"
expect "closed PR kept" "task-3: PR #3 closed unmerged"
expect "no PR kept" "task-4: no PR"
expect "dirty kept" "task-5: uncommitted changes"
expect "unpushed commits kept" "task-6: branch has commits outside merged PR #6"
expect "brand-new worktree kept" "task-7: no PR"
expect "running process kept" "task-8: in use by sleep"
expect "worktree named by --keep kept" "task-9: named by --keep"
expect "stale agent removed" "removed .*agent-stale"
expect "fresh agent kept" "agent-fresh: agent worktree under 24h old"
expect "agent with commits kept" "agent-ahead: agent branch 1 ahead"
expect "pushed agent kept" "agent-pushed: agent branch was pushed"
expect "worktree outside .claude/worktrees kept" "keep    outside: outside .claude/worktrees"

if exists "$merged" || git show-ref --quiet refs/heads/chore/1-case; then echo "FAIL  merged worktree or branch survived"; failures=$((failures + 1)); fi
if ! exists "$open" || ! exists "$brand_new" || ! exists "$outside"; then echo "FAIL  a kept worktree is gone"; failures=$((failures + 1)); fi

occupied="$(task 10)"; commit_in "$occupied" work; pr 10 MERGED "$(pushed_head "$occupied")"
output="$(cd "$occupied" && "$script" clean --yes)"
expect "worktree the script runs in kept" "task-10: in use by"
expect "--keep lasts one run" "removed .*task-9 "
if ! exists "$occupied"; then echo "FAIL  the worktree the script ran in is gone"; failures=$((failures + 1)); fi

output="$(cd "$occupied" && "$script" new 11 chore/11-case 2>/dev/null)"
expect "new from a worktree lands under the primary" "^$tmp/repo/.claude/worktrees/task-11$"

for bad in "clean --keep" "clean --nope"; do
  if "$script" $bad 2>/dev/null; then echo "FAIL  '$bad' was accepted"; failures=$((failures + 1)); else echo "ok    '$bad' rejected"; fi
done

[[ $failures -eq 0 ]] && echo "All cases passed"
exit "$failures"
