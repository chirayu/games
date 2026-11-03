# Letterpress English dictionary

`words.txt` is generated from the official English Speller Database (ESDB,
formerly SCOWL) release `rel-2026.02.25`, commit
`7e99edab8e32f9f9ea2b15f249ca8d4d67237410`.

- Source: <https://github.com/en-wl/wordlist/archive/refs/tags/rel-2026.02.25.tar.gz>
- Source SHA-256: `74e7cc3e9e03e609c1c74bb7e8862fcd988cdd64768dcbee4611581b7e633852`
- Generated SHA-256: `a64b8817f69d554f09165d461a1c115774147f3cb80790879757331f26e62558`
- ESDB size: 80
- Spellings: American, British (`-ise` and `-ize`), Canadian, Australian
- Regions: US, GB, CA, AU
- Variant level: 1

The generator deaccents the upstream output, converts it to uppercase, keeps
only words matching `^[A-Z]+$`, sorts with the C locale, and removes duplicates.
The resulting 222,023 words can all be formed from literal Letterpress tiles;
punctuation, spaces, digits, and non-ASCII characters are excluded.

Run `./generate.sh` from any directory to intentionally refresh and verify the
pinned artifact. Updating ESDB requires reviewing the new upstream notices,
changing the pinned release/revision and checksums, regenerating the files, and
running the complete backend, frontend, and end-to-end suites.

The full upstream notice is preserved verbatim in `LICENSE-ESDB.txt` and ships
with source and container builds.
