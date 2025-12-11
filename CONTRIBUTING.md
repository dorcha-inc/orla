# contributing to orla

Thank you so much for considering contributing to orla! orla is designed to be a community-focused project and runs on individual contributions from amazing people around the world. This document provides guidelines and instructions for contributing to the project.

## code of conduct

This project adheres to a Code of Conduct that all contributors are expected to follow. Please read [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) before contributing.

## getting started

1. Fork the repository on GitHub
2. Clone your fork locally:
   ```bash
   git clone https://github.com/YOUR_USERNAME/orla.git
   cd orla
   ```
3. Add the upstream remote:
   ```bash
   git remote add upstream https://github.com/dorcha-inc/orla.git
   ```

## development setup

### prerequisites

orla requires go 1.23+. Please make sure you have Go installed. Check with 

```bash
go version
```

orla also required [golangci-lint](https://golangci-lint.run/) for linting. 

### setup steps

first install dependencies by running

```bash
make deps
```

then you can verify the setup using

```bash
make lint
```

and

```bash
make test
```

try building the project using

```bash
make build
```

## making changes

### Workflow

1. create a branch from `main`:

```bash
git checkout -b <your_github_username>/<descriptive_name>
```

2. write or update tests for your changes

3. run tests and linter:

   ```bash
   make test
   make lint
   ```

4. commit your changes with clear, descriptive commit messages:
   ```bash
   git commit -m "<message>"
   ```

### commit message guidelines

we follow [conventional commit](https://www.conventionalcommits.org/en/v1.0.0/) message style:

- `feat: add hot reload support for tools`
- `fix: resolve shebang parsing edge case`
- `docs: update README with installation instructions`
- `test: add tests for tool discovery`
- `refactor: simplify config loading logic`

## submitting changes

### pull request process

1. push your branch to your fork:
   ```bash
   git push origin feature/your-feature-name
   ```

2. create a pull request on github.

3. please ensure CI checks pass before asking for a review.

4. please address reviewer feedback

### pull request checklist

Before submitting, make sure:

- [ ] You've added tests for new functionality
- [ ] All tests pass (`make test`)
- [ ] Linting passes (`make lint`)
- [ ] Code is formatted (`make format`)
- [ ] Documentation is updated if needed
- [ ] Commit messages are clear and descriptive


## testing

run all tests using

```bash
make test
```

run tests with verbose output using

```bash
VERBOSE=1 make test
```

## areas for contribution

we welcome contributions in many areas including bug fixes, features, documentation,
tests, tooling, code quality, security patches, and performance optimizations.

## recognition

contributors will be recognized in

1. github contributors list
2. release notes

Thank you for contributing to orla!
