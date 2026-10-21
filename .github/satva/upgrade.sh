#!/usr/bin/env bash
# Move Satva's patches onto a new upstream Obot release, without a checkout.
#   bash .github/satva/upgrade.sh v0.25.6
# Remotes expected: origin = obot-platform/obot (upstream), satva = satva-git/obot-satva (fork).
# Result: branch satva/upgrade-<tag> that is a normal descendant of satva/main, so it lands as an
# ordinary PR (no force-push). CI builds and tests it; deploy waits for the PR merge + approval.
set -euo pipefail
NEW=${1:?usage: upgrade.sh <upstream tag, e.g. v0.25.6>}
git fetch -q origin "refs/tags/$NEW:refs/tags/$NEW"
git fetch -q satva "refs/heads/satva/main:refs/remotes/satva/satva/main"
MAIN_REF=refs/remotes/satva/satva/main   # git replay needs a ref, not a sha
MAIN=$(git rev-parse $MAIN_REF)
BASE=$(git describe --tags --abbrev=0 --match 'v*' "$MAIN")
echo "patches on satva/main (base $BASE):"
git log --oneline "$BASE..$MAIN"
# git replay stops with an error on a conflict; then cherry-pick by hand in a worktree.
REPLAYED=$(git replay --onto "$NEW" "$BASE..$MAIN_REF" | awk '{print $3}')
# Merge commit: content = replayed patches on $NEW, parents = replayed + current satva/main,
# so satva/main fast-forwards to it through a PR.
UP=$(git commit-tree "$REPLAYED^{tree}" -p "$REPLAYED" -p "$MAIN" -m "chore: upgrade Satva patches to Obot $NEW")
git push -q satva "refs/tags/$NEW"
git push -q satva "$UP:refs/heads/satva/upgrade-$NEW"
cat <<EOF

Pushed satva/upgrade-$NEW ($UP). CI is building and testing it now.
Before merging: read the upstream release notes from $BASE to $NEW. A minor-version jump
(e.g. 0.25 -> 0.26) migrates the database and the old image cannot read it back.
Open the PR (deploy still needs approval after merge):
  gh pr create -R satva-git/obot-satva -B satva/main -H satva/upgrade-$NEW -t "Upgrade to Obot $NEW" -b "Replays Satva patches onto $NEW."
EOF
