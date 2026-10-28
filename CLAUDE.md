# spmeta

Go CLI that prints Spotify's currently playing track. Design docs are in `docs/` (start at `docs/README.md`); the task list and progress are in `odd/tasks/spmeta-v1.md`.

## Before every commit

```bash
go test ./...
go vet ./...
golangci-lint run
```

## Conventions

- Test-first per task: write the failing test, watch it fail, then implement.
- Conventional Commits (`feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `ci`, `perf`). They drive SemVer and the generated CHANGELOG, and CI rejects other subjects.
- Public contract (flags, exit codes, JSON schema) is defined in `docs/design/cli.md` and `docs/design/data-model.md`; change the docs together with the code.
- Third-party runtime dependencies need an ADR (`docs/decisions/0006-dependencies.md`).
- All code, comments, and UI strings are in English. Lowercase file names.
- Never log tokens, authorization codes, or secrets, not even with `--debug`.
- Never commit `.claude/odoo-timesheet/` or `.claude/*.local.json`.
