#!/usr/bin/env bash
# Prints the GitHub Release body for a tag (for example v0.3.0): the matching CHANGELOG.md
# section, followed by install and verification notes. Fails if the tag has no section, so a
# release can never go out with generic or empty notes.
set -euo pipefail

tag="${1:?usage: release_notes.sh vX.Y.Z}"
version="${tag#v}"
changelog="$(dirname "$0")/../CHANGELOG.md"

section="$(tr -d '\r' <"$changelog" | awk -v ver="$version" '
  /^## \[/ {
    if (found) exit
    if (index($0, "## [" ver "]") == 1) { found = 1; next }
  }
  found { print }
')"

# Drop leading/trailing blank lines.
section="$(printf '%s\n' "$section" | sed -e '/./,$!d' | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}')"

if [ -z "$section" ]; then
  echo "release_notes.sh: no '## [${version}]' section with content in CHANGELOG.md" >&2
  exit 1
fi

cat <<NOTES
## go-opus-codec ${tag}

A pure Go Ogg/Opus parser, decoder, encoder and repacketizer (\`CGO_ENABLED=0\`).

${section}

---

### Install

\`\`\`bash
go get github.com/selawe/go-opus-codec@${tag}
\`\`\`

Requires Go 1.24+ on a 64-bit target (\`amd64\` or \`arm64\`); 32-bit builds are rejected at compile time.

### Verified for this release

This release is published only after \`go test ./...\` and the RFC 6716 / RFC 8251 conformance suite pass on the tagged commit. The 12 official test vectors decode within ±1 LSB of the reference output; see [conformance_matrix.md](conformance_matrix.md).

Running the race detector on code that uses this library needs \`-gcflags=all=-d=checkptr=0\`; the README explains why.

The full change list follows below.
NOTES
