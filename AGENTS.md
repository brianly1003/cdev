# Repository Guidelines

## Project Structure & Module Organization
- `cmd/cdev/` is the main CLI entrypoint; the production binary is built from here.
- `internal/` holds core application code (app orchestration, adapters, servers, RPC, domain types).
- `frontend/` is the React + Vite UI used by Wails; `wails.json` defines the desktop build wiring.
- `api/` stores generated Swagger/OpenRPC artifacts; `docs/` contains specs and architecture notes.
- `configs/` and `config.yaml` provide sample/default configuration; `assets/` holds static images/icons.
- `scripts/`, `tools/`, and `test/` contain helper scripts, tooling, and coverage outputs.

## Build, Test, and Development Commands
- `make build` builds the CLI to `bin/cdev`.
- `make run` runs in terminal mode; `make run-headless` runs Claude as a background subprocess.
- `make run-bg` starts a background server; stop with `make stop`.
- `make run-debug` builds with the `deadlock` tag for debugging.
- `make test`, `make test-race`, `make test-coverage` run Go tests (HTML report at `test/coverage.html`).
- `make fmt`, `make vet`, `make lint`, `make tidy` enforce formatting and code quality.
- `make swagger` regenerates OpenAPI docs; `make openrpc` fetches OpenRPC from a running server.
- Frontend (from `frontend/`): `npm run dev`, `npm run build`, `npm run preview`.

## Coding Style & Naming Conventions
- Go code is formatted with `gofmt` (`make fmt`) and linted by `golangci-lint` (`make lint`).
- Prefer standard Go naming: exported identifiers `CamelCase`, packages `lowercase`.
- Tests live alongside code in `*_test.go` files.

## Testing Guidelines
- Run the full suite with `make test`; target a package with `make test-pkg PKG=./internal/...`.
- A coverage check is available via `make test-coverage-check` (see `.testcoverage.yml`).
- For single tests: `go test -v -run TestName ./internal/<package>`.

## Commit & Pull Request Guidelines
- Commit history follows conventional commits (e.g., `feat:`, `fix:`, `docs:`, `refactor:`, `chore:`).
- No PR template is present; include a short summary, test commands run, and UI screenshots when relevant.
- If you change APIs, update Swagger/OpenRPC outputs and related docs.

## Configuration & Security Notes
- Config resolution order: `--config`, `./config.yaml`, `~/.cdev/config.yaml`, `/etc/cdev/config.yaml`;
  environment overrides use the `CDEV_` prefix.
- Review `docs/security/SECURITY.md` before changing auth, CORS, or networking behavior.

## Agent-Specific Instructions
- Do not create commits unless explicitly asked by the user.
- Always run `make build` before saying work is done.
