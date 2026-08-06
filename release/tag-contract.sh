#!/bin/sh
# Refuse a tag that does not satisfy the release contract, before the release
# workflow spends a gate run, five builds, and an attestation on it.
#
# Two clauses, both stated in CONTRIBUTING.md:
#   - the tag is annotated
#   - CHANGELOG.md has a section for the version the tag names
#
# The publish step still lifts that section out of CHANGELOG.md itself and
# refuses an empty one. This asks the cheaper question early; that one stays the
# owner of parsing the section it publishes.
#
#   sh release/tag-contract.sh v0.3.0
#   sh release/tag-contract.sh --prospective v0.3.0
#   sh release/tag-contract.sh --self-check
set -eu

# Both clauses, against the repository in the current directory. Prints one line
# per clause refused, and returns non-zero if any did. Every refusal names what
# to do, because the two are fixed differently: one is retagged, one is written.
check() {
	tag="$1"
	refused=0

	if [ "$(git cat-file -t "$tag" 2>/dev/null || echo missing)" != tag ]; then
		echo "tag $tag is not an annotated tag: delete it and retag with git tag -a"
		refused=1
	fi

	check_changelog "$tag" || refused=1

	return "$refused"
}

check_changelog() {
	tag="$1"
	if ! printf '%s\n' "$tag" | awk '/^v[0-9]+\.[0-9]+\.[0-9]+$/ { valid = 1 } END { exit !valid }'; then
		echo "tag $tag is not v<major>.<minor>.<patch>: choose a semantic version"
		return 1
	fi

	# Versions are digits and dots, so escaping the dots is the whole quoting
	# problem: unescaped, 0.3.0 would also match a heading for 0x3y0.
	version="${tag#v}"
	escaped="$(printf '%s' "$version" | sed 's/\./\\./g')"
	awk -v heading="## [$version]" '
		$0 == heading || index($0, heading " ") == 1 { found = 1; next }
		found && /^## \[/ { exit }
		found && $0 !~ /^[[:space:]]*$/ { body = 1 }
		END { exit !(found && body) }
	' CHANGELOG.md || {
		echo "CHANGELOG.md has no non-empty section for $version: write one before tagging"
		return 1
	}
}

# Builds a repository the clauses can be run against, because the workflow this
# script gates cannot be run locally and a tag push is too expensive to be the
# first test. Three tags: one meeting the contract, one failing each clause.
self_check() {
	work="$(mktemp -d)"
	trap 'rm -rf "$work"' EXIT
	(
		cd "$work"
		git init -q .
		git config user.email contract@example.test
		git config user.name contract
		printf '## [1.0.0]\n\nfirst\n\n## [2.0.0]\n\nsecond\n' >CHANGELOG.md
		git add CHANGELOG.md
		git commit -qm fixture
		git tag -a v1.0.0 -m 'annotated, section present'
		git tag v2.0.0
		git tag -a v3.0.0 -m 'annotated, section absent'
	)

	failures=0
	expect() {
		label="$1"
		tag="$2"
		want_code="$3"
		want_text="$4"
		if output="$(cd "$work" && check "$tag" 2>&1)"; then
			code=0
		else
			code=1
		fi
		if [ "$code" != "$want_code" ]; then
			echo "FAIL $label: exit $code, want $want_code"
			failures=$((failures + 1))
			return 0
		fi
		case "$output" in
		*"$want_text"*) echo "ok   $label" ;;
		*)
			echo "FAIL $label: output '$output' does not mention '$want_text'"
			failures=$((failures + 1))
			;;
		esac
	}

	expect "annotated tag with a section is accepted" v1.0.0 0 ""
	expect "lightweight tag is refused" v2.0.0 1 "is not an annotated tag"
	expect "missing changelog section is refused" v3.0.0 1 "no non-empty section for 3.0.0"
	if ! (cd "$work" && check_changelog v1.0.0); then
		echo "FAIL prospective tag with a section was refused"
		failures=$((failures + 1))
	fi

	if [ "$failures" -ne 0 ]; then
		echo "$failures self-check failure(s)"
		exit 1
	fi
	echo "self-check passed"
}

if [ "${1:-}" = --self-check ]; then
	self_check
	exit 0
fi

if [ "${1:-}" = --prospective ]; then
	if [ $# -ne 2 ]; then
		echo "usage: tag-contract.sh --prospective <tag>" >&2
		exit 2
	fi
	check_changelog "$2"
	echo "prospective tag contract holds for $2"
	exit 0
fi

if [ $# -ne 1 ]; then
	echo "usage: tag-contract.sh <tag> | --prospective <tag> | --self-check" >&2
	exit 2
fi

if ! check "$1"; then
	exit 1
fi
echo "tag contract holds for $1"
