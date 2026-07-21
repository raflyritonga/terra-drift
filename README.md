# terra-drift

Someone changes infrastructure by hand in the AWS console. Terraform's code doesn't know, and the next `apply` quietly reverts it. terra-drift detects that drift, shows a short report, and asks you which side to trust — the **code** or the **live infrastructure**. Only if you choose "live" does it edit the Terraform to match reality and open a pull request.

**Docs: https://raflyritonga.github.io/terra-drift/**

## Install

Prebuilt binaries for Linux/macOS/Windows are on the [Releases page](https://github.com/raflyritonga/terra-drift/releases) (each archive has both binaries). Or from source:

```sh
go install github.com/raflyritonga/terra-drift/cmd/terra-drift@latest
go install github.com/raflyritonga/terra-drift/cmd/terra-drift-mcp@latest
```

Or build locally:

```sh
go build -o terra-drift     ./cmd/terra-drift    # the client (runs in CI)
go build -o terra-drift-mcp ./cmd/terra-drift-mcp # the model server (its own box)
```

Cut a release by pushing a tag (`git tag v0.1.0 && git push origin v0.1.0`) — CI builds every platform and publishes the archives.

## Quick look

```sh
terra-drift check --dir envs/prod    # detect + short report (file:line); exit 0 clean / 2 drift / 1 error
terra-drift sync  --dir envs/prod    # detect → report → ask which side to trust → act
# add --explain to either: the model server appends a short read-only summary of the drift
```

- `--trust code` — change nothing (next apply reverts the live drift)
- `--trust live` — rewrite code to match reality, open a PR
- `--trust partial --live aws_x.y` — trust reality for named resources only

In a terminal it prompts; in CI it prints the report and exits 2 until you pass `--trust`.

## Layout

```
cmd/terra-drift       the client: detect, classify, edit, open PR
cmd/terra-drift-mcp   the server: talks to your model, returns structured edits
internal/             shared + per-binary packages
tests/                all tests
docs/                 the documentation site (GitHub Pages)
```

Setup, hosting, secrets, and copy-paste CI configs are in the [docs site](https://raflyritonga.github.io/terra-drift/).

## Test

```sh
go test ./...          # unit, golden, and cross-binary e2e (stdio + HTTP)
go test ./... -short   # skip the e2e tests that build the server binary
```
