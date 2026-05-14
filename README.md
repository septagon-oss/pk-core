# pk-core

Minimal OSS core for PlatformKit's modular application framework.

This first cut is intentionally small. It proves the public architecture spine
without pulling in the private product workspace:

- modules declare metadata, providers, invocations, provided ports, and required
  ports
- bundles compose modules into catalogs
- catalogs validate duplicate module IDs and expose defaults
- composition builds a dependency-ordered module plan
- typed ports use Go interfaces without importing implementation packages

The paid PlatformKit distribution can fork this repo or build on top of it by
adding private modules, providers, adapters, deployment targets, and hosted
workflows.

This is one of the five minimum OSS repos:

- `pk-shared`
- `pk-core`
- `pk-design`
- `pk-modules`
- `pk-apps`

See [docs/OPEN_CORE.md](docs/OPEN_CORE.md) for the Pro extension boundary.

## Quickstart

```bash
go test ./...
go run ./examples/minimal
```

## Public Contract

The initial stable surface is the `pkg/module` package:

- `module.Module`
- `module.Composable`
- `module.Bundle`
- `module.Catalog`
- `module.PortOf`, `module.Provide`, `module.Require`
- `module.Compose`

`module.Module` is the concrete base type. Paid/private modules can embed it
and extend it without changing the OSS contract.

Everything else will be added only when it is necessary to keep the first
developer path complete.
