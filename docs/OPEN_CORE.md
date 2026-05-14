# Open-Core Boundary

This OSS repository is the upstream framework core.

The Pro version may be a downstream fork or a private distribution that imports
this module. In both cases, Pro should extend the OSS framework through the same
contracts public users get:

- embed `module.Module` for paid modules
- register paid modules through `module.Bundle`
- compose paid modules through `module.Catalog`
- declare paid module dependencies through typed ports
- add Pro providers, adapters, deployment targets, and presets outside the OSS
  core

The Pro version should avoid privately redefining:

- module metadata semantics
- bundle/catalog behavior
- typed port dependency behavior
- dependency validation and ordering

When those contracts need to change, change OSS first and let Pro consume the
new public tag.

## Embedding Pattern

```go
type BillingModule struct {
    module.Module
    Provider string
}

func NewBillingModule() *BillingModule {
    return &BillingModule{
        Module: *module.Must(module.Metadata{ID: "billing"}),
        Provider: "stripe",
    }
}
```

