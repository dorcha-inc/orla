# orla git hooks

git hooks are automatically used from this directory via `core.hooksPath` configuration.

## setup

run this once to configure git to use these hooks:

```bash
git config core.hooksPath .githooks
```

## pre-commit hook

the pre-commit hook runs three checks before each commit:

1. `gitleaks` (if installed) to detect potential secrets
2. `make lint` to check code quality
3. `make test` to ensure tests pass

to skip tests only, set `SKIP_TESTS=true` before committing.

```bash
SKIP_TESTS=true git commit -m "your message"
```

to skip all checks, use the `--no-verify` flag

```bash
git commit --no-verify -m "your message"
```

for secret detection, install [gitleaks](https://github.com/gitleaks/gitleaks). 

using pre-commit hooks saves us time by catching issues locally rather than waiting for CI to fail.
