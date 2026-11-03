#!/bin/sh
set -eu

release=rel-2026.02.25
revision=7e99edab8e32f9f9ea2b15f249ca8d4d67237410
source_url="https://github.com/en-wl/wordlist/archive/refs/tags/$release.tar.gz"
source_sha256=74e7cc3e9e03e609c1c74bb7e8862fcd988cdd64768dcbee4611581b7e633852
output_sha256=a64b8817f69d554f09165d461a1c115774147f3cb80790879757331f26e62558

directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT HUP INT TERM

curl -fsSL "$source_url" -o "$temporary/esdb.tar.gz"
actual_source=$(openssl dgst -sha256 "$temporary/esdb.tar.gz" | awk '{print $NF}')
if [ "$actual_source" != "$source_sha256" ]; then
  echo "ESDB source checksum mismatch: got $actual_source" >&2
  exit 1
fi

tar -xzf "$temporary/esdb.tar.gz" -C "$temporary"
source_directory="$temporary/wordlist-$release"
cd "$source_directory"
./combine.py create-db scowl.db
./scowl --db scowl.db word-list 80 A,B,Z,C,D 1 \
  --regions US,GB,CA,AU --deaccent \
  | LC_ALL=C tr '[:lower:]' '[:upper:]' \
  | LC_ALL=C awk '/^[A-Z]+$/' \
  | LC_ALL=C sort -u > "$temporary/words.txt"

actual_output=$(openssl dgst -sha256 "$temporary/words.txt" | awk '{print $NF}')
if [ "$actual_output" != "$output_sha256" ]; then
  echo "generated dictionary checksum mismatch: got $actual_output" >&2
  exit 1
fi

cp "$temporary/words.txt" "$directory/words.txt"
cp Copyright "$directory/LICENSE-ESDB.txt"
printf 'Generated ESDB %s (%s): %s\n' "$release" "$revision" "$actual_output"
