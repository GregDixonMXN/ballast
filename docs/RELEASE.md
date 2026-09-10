# Release a local candidate

## Verify and package

```bash
./scripts/check.sh
./scripts/check-postgres.sh
./scripts/build.sh
python3 tests/release_smoke.py --bin-dir bin
./scripts/package.sh --skip-build
./scripts/check-container.sh
```

The output is `dist/ballast-<VERSION>-linux-x86_64.tar.gz` plus an archive checksum.
It contains three Go binaries, the Next.js standalone server and static assets,
documentation, a launcher, dependency lock metadata, and per-file checksums.
Node.js 24 and Git are runtime dependencies, not bundled compilers.

Extract into a fresh directory, verify both the archive and included `SHA256SUMS`,
and run the smoke test against the extracted `bin/`. Then launch the extracted
package with an isolated `BALLAST_STATE_DIR` and verify the actual dashboard.
Do not test an old package after changing the source; rebuild it.

## Publication boundary

This workflow creates local artifacts only. It does not commit, tag, push,
publish a package, provision hosting, or upload source/state to a third party.
Before public distribution, the owner must choose project licensing, resolve
third-party notices, select a signing/update/support channel, and run hosted CI.
Do not label an unsigned local candidate as an externally audited production launch.

## Evidence

Record toolchain versions, exact checks, package checksums, runtime assumptions,
known limits, and any skipped checks in `docs/VERIFICATION.md`. Include only facts
that were actually verified, not a checklist relabeled as successful testing.
