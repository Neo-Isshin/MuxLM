# Catalog audit

This read-only maintainer command compares `catalog-v2.json` with the current
[models.dev API](https://models.dev/):

```sh
go run ./cmd/catalog-audit
```

It reports current model IDs missing upstream and tool-capable text models that
may deserve review. It never edits the catalog or changes a provider's default
model. MuxLM keeps an explicit provider/plan allowlist because an upstream
model listing alone does not prove that the model works with a particular
billing plan or CLI protocol.

Useful options:

```sh
go run ./cmd/catalog-audit --limit 20
go run ./cmd/catalog-audit --strict
go run ./cmd/catalog-audit --source ./api.json
```

`--strict` fails only for unacknowledged missing IDs. Known documentation gaps
remain visible in the report without breaking the check.
