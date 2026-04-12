# Repository Instructions

Always use Conventional Commit format for commit messages and pull request titles.

Accepted format:

- `feat(scope): short summary`
- `fix(scope): short summary`
- `docs(scope): short summary`
- `refactor(scope): short summary`
- `perf(scope): short summary`
- `test(scope): short summary`
- `build(scope): short summary`
- `ci(scope): short summary`
- `chore(scope): short summary`

Rules:

- Prefer `feat:` for user-visible behavior changes and `fix:` for bug fixes.
- Keep the summary imperative and concise.
- Use lowercase types and scopes.
- Assume pull request titles will be used for release automation.
- Do not use a breaking-change marker (`!`) unless the maintainer explicitly intends a breaking release.
- Before proposing a commit or PR title, rewrite it into Conventional Commit format if needed.

Examples:

- `feat(ui): add panel maximize toggle`
- `fix(process): stabilize scrollback IDs`
- `docs(readme): explain release commit conventions`

## License

This project is released under the MIT License. By contributing, you agree that your contributions will be licensed under the same MIT License (inbound = outbound).
