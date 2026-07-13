# Release process

## Versioning

ServerVault follows semantic versioning (`vMAJOR.MINOR.PATCH`).
Pre-1.0, minor version bumps may include breaking changes to the Go CLI
while it's stabilizing — see [`ROADMAP.md`](../ROADMAP.md) for what
"1.0" is scoped to mean.

## Where releases come from

- `main` — tagged releases of the stable shell implementation.
- `go-rewrite` — pre-release Go builds (`vX.Y.Z-alpha`, `-beta`) until
  the rewrite reaches parity and merges into `main`.

## Cutting a release

1. Confirm `main` (or `go-rewrite`, for a pre-release) is green in CI.
2. Update [`CHANGELOG.md`](../CHANGELOG.md) with the notable changes.
3. Tag the release:

   ```bash
   git tag -a vX.Y.Z -m "vX.Y.Z"
   git push origin vX.Y.Z
   ```

4. Pushing the tag triggers `.github/workflows/release.yml`, which:
   - builds `linux/amd64` and `linux/arm64` binaries with version
     metadata baked in via `-ldflags`,
   - generates SHA-256 checksums,
   - uploads them as a workflow artifact, and
   - opens a **draft** GitHub release with auto-generated release
     notes.
5. **The release is not published automatically.** A maintainer
   reviews the draft, edits the notes if needed, and publishes it by
   hand from the GitHub UI.

The workflow can also be run manually (`workflow_dispatch`) against an
existing tag, e.g. to rebuild artifacts without re-tagging.

## Why the draft step is manual

A backup tool's releases are trusted to run as root against production
data. Auto-publishing removes the last human checkpoint before a build
is presented to users as ready to install — that checkpoint stays
manual on purpose.

## Post-release

- Confirm the published binaries' checksums match the workflow's
  `checksums.txt`.
- If the release came from `go-rewrite` and reached parity with
  `main`, follow the merge plan in `ROADMAP.md`'s v1.0.0 milestone
  rather than merging ad hoc.
