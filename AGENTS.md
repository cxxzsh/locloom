# Project Conventions

## Git Commit Messages

Always use [Conventional Commits](https://www.conventionalcommits.org/) format:

```text
<type>(<scope>): <summary>
```

- **Types**: `feat`, `fix`, `refactor`, `perf`, `docs`, `test`, `chore`, `build`, `ci`, `style`
- **Scope**: affected package or module (e.g. `app`, `format`, `locale`, `provider`, `tsxml`, `release`)
- **Summary**: concise imperative description in English (or Chinese if requested)
- Examples:
  - `feat(format): default Save Back to *_updated folder with centralized backups`
  - `fix(app): scope locale preset profiles to loaded project`
  - `docs(repo): document conventional commit guidelines`

## Local Development & Verification

- Invoke Go via `mise` when bare `go` is unavailable: `mise x go@1.27.3 -- go test -race ./...`
- Do not commit local fixture directories such as `test_data/`.
- `.ts` files in fixtures/tests are Qt Linguist XML translation files, not TypeScript sources.
