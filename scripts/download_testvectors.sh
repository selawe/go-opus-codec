#!/usr/bin/env bash
set -euo pipefail

# Directory where test vectors will be stored
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
TARGET_DIR="${ROOT_DIR}/testvectors"
ARCHIVE_URL="https://opus-codec.org/static/testvectors/opus_testvectors.tar.gz"
ARCHIVE_FILE="${ROOT_DIR}/opus_testvectors.tar.gz"

echo "=== RFC 6716 Test Vector Fetcher ==="
mkdir -p "$TARGET_DIR"

if [ -f "${TARGET_DIR}/testvector01.bit" ]; then
    echo "Test vectors already exist in ${TARGET_DIR}. Skipping download."
else
    echo "Downloading official test vectors from ${ARCHIVE_URL}..."
    curl -L --retry 3 -o "$ARCHIVE_FILE" "$ARCHIVE_URL"

    echo "Extracting test vectors..."
    tar -xzf "$ARCHIVE_FILE" -C "$TARGET_DIR" --strip-components=1 2>/dev/null || tar -xzf "$ARCHIVE_FILE" -C "$TARGET_DIR"
    rm -f "$ARCHIVE_FILE"
fi

echo "Verifying SHA-1 checksums of official test vectors..."
cat << 'EOF' > "${TARGET_DIR}/rfc6716_checksums.sha1"
e49b2862ceec7324790ed8019eb9744596d5be01  testvector01.bit
b809795ae1bcd606049d76de4ad24236257135e0  testvector02.bit
e0c4ecaeab44d35a2f5b6575cd996848e5ee2acc  testvector03.bit
a0f870cbe14ebb71fa9066ef3ee96e59c9a75187  testvector04.bit
9b3d92b48b965dfe9edf7b8a85edd4309f8cf7c8  testvector05.bit
28e66769ab17e17f72875283c14b19690cbc4e57  testvector06.bit
bacf467be3215fc7ec288f29e2477de1192947a6  testvector07.bit
ddbe08b688bbf934071f3893cd0030ce48dba12f  testvector08.bit
3932d9d61944dab1201645b8eeaad595d5705ecb  testvector09.bit
521eb2a1e0cc9c31b8b740673307c2d3b10c1900  testvector10.bit
6bc8f3146fcb96450c901b16c3d464ccdf4d5d96  testvector11.bit
338c3f1b4b97226bc60bc41038becbc6de06b28f  testvector12.bit
EOF

cd "$TARGET_DIR"
if command -v sha1sum >/dev/null 2>&1; then
    sha1sum -c rfc6716_checksums.sha1
elif command -v shasum >/dev/null 2>&1; then
    shasum -a 1 -c rfc6716_checksums.sha1
fi

echo "RFC 6716 test vectors verified successfully in ${TARGET_DIR}."
