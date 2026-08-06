#!/bin/sh
# Prepare reviewable release metadata. This creates no tag and publishes
# nothing; merging the resulting PR is the human authorization to tag.
set -eu

script_dir="$(CDPATH= cd "$(dirname "$0")" && pwd)"

die() {
	echo "$1; next: $2" >&2
	exit 1
}

self_check() {
	work="$(mktemp -d)"
	trap 'rm -rf "$work"' EXIT
	mkdir -p "$work/release"
	cp "$script_dir/prepare.sh" "$script_dir/tag-contract.sh" "$work/release/"
	(
		cd "$work"
		git init -q .
		git branch -M main
		git config user.email release@example.test
		git config user.name release
		printf '# Changelog\n\n## [Unreleased]\n\n### Added\n\n- release note\n' >CHANGELOG.md
		git add .
		git commit -qm baseline
		git init --bare -q "$work/.git/test-origin"
		git remote add origin "$work/.git/test-origin"
		git push -qu origin main
		sh release/prepare.sh 9.9.9 >/dev/null
		test "$(git branch --show-current)" = release/9.9.9
		git show-ref --verify --quiet refs/tags/v9.9.9 && exit 1
		grep -q '^## \[9\.9\.9\]' CHANGELOG.md
		git log -1 --format=%s | grep -q '^chore: prepare v9\.9\.9$'
	)
	echo "prepare self-check passed"
}

if [ "${1:-}" = --self-check ]; then
	self_check
	exit 0
fi

[ $# -eq 1 ] || { echo "usage: release/prepare.sh <major.minor.patch>" >&2; exit 2; }
version="$1"
tag="v$version"
printf '%s\n' "$version" | awk '/^[0-9]+\.[0-9]+\.[0-9]+$/ { valid = 1 } END { exit !valid }' \
	|| die "version must be major.minor.patch" "choose a semantic version"

root="$(git rev-parse --show-toplevel 2>/dev/null)" || die "not inside a Git repository" "run from the specd checkout"
cd "$root"
[ -z "$(git status --porcelain)" ] || die "working tree is dirty" "commit or stash the current changes"
branch="$(git symbolic-ref --short HEAD 2>/dev/null || true)"
[ "$branch" = main ] || die "current branch is $branch, not main" "switch to main"
git show-ref --verify --quiet "refs/tags/$tag" && die "$tag already exists locally" "choose the next version"
[ -n "$(git remote get-url origin 2>/dev/null || true)" ] || die "origin is not configured" "configure the release remote"
if git ls-remote --exit-code --tags origin "refs/tags/$tag" >/dev/null 2>&1; then
	die "$tag already exists on origin" "choose the next version"
else
	status=$?
	[ "$status" -eq 2 ] || die "origin could not be checked for $tag" "restore remote access and retry"
fi

today="$(date -u +%Y-%m-%d)"
tmp="$(mktemp "${TMPDIR:-/tmp}/specd-changelog.XXXXXX")"
trap 'rm -f "$tmp"' EXIT HUP INT TERM
awk -v version="$version" -v today="$today" '
	/^## \[Unreleased\]/ && !done {
		print "## [Unreleased]"
		print ""
		print "## [" version "] — " today
		done = 1
		next
	}
	{ print }
	END { if (!done) exit 1 }
' CHANGELOG.md >"$tmp" || die "CHANGELOG.md has no Unreleased section" "add human-written release notes"
mv "$tmp" CHANGELOG.md
trap - EXIT HUP INT TERM

if ! sh release/tag-contract.sh --prospective "$tag"; then
	git restore CHANGELOG.md
	die "prospective tag contract failed" "add non-empty human-written Unreleased notes"
fi
git switch -c "release/$version"
git add CHANGELOG.md
git commit -m "chore: prepare $tag"
echo "prepared release/$version; next: gh pr create --base main --head release/$version --title 'Release $tag'"
