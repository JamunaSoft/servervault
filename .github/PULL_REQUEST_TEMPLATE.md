## Summary

<!-- What does this change do and why? -->

## Type of change

- [ ] Bug fix
- [ ] New feature
- [ ] Documentation
- [ ] CI / tooling
- [ ] Refactor (no behavior change)

## Affected area

- [ ] Shell implementation (`bin/`, `install.sh`, `systemd/`)
- [ ] Go rewrite (`cmd/`, `internal/`)
- [ ] Docs (`docs/`)
- [ ] CI (`.github/workflows/`)

## Checklist

- [ ] I read `CONTRIBUTING.md`.
- [ ] I targeted the correct branch (`main` for stable shell fixes, `go-rewrite` for Go work).
- [ ] No secrets, credentials, private hostnames, or internal paths are included in this diff.

### If Go code changed

- [ ] `gofmt -w .`
- [ ] `go vet ./...`
- [ ] `go test ./...`
- [ ] `go build ./cmd/servervault`

### If shell code changed

- [ ] `bash -n bin/* install.sh`
- [ ] `shellcheck bin/* install.sh`

## Testing performed

<!-- Describe how you verified this change (commands run, environment, manual steps). -->

## Related issues

<!-- e.g. Closes #123 -->
