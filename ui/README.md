# forgejo-github-bridge console

The read-only web console of forgejo-github-bridge: Angular 22, Tailwind 4 and
the metapatrol brand (`@metapatrol/ng`, `@metapatrol/style`,
`@metapatrol/brand`). The build is embedded into the bridge binary
(`internal/ui`), which serves it at `/` on its console listener, beside the
API at `/api/v1`. See [docs/console.md](../docs/console.md).

The API client in `src/app/api` is generated from
[`../api/openapi-v1.yaml`](../api/openapi-v1.yaml) before every build, test,
lint and dev server start; it is not committed.

## Requirements

- Node.js ≥ 22.22.3 (`.nvmrc`).
- `NPM_TOKEN`: a Forgejo token with `read:package` scope, for the
  `@metapatrol/*` packages from the forge's npm registry (`.npmrc`).
- The forge's CA, where the system does not trust it already:
  `NODE_EXTRA_CA_CERTS=/path/to/ca.pem`.

## Commands

```bash
npm ci            # install
npm run lint      # eslint, templates included
npm test          # unit tests (vitest, headless)
npm run build     # → ../internal/ui/dist/console
npm start         # dev server on :4200, /api proxied to a local console listener
```

`npm start` passes `/api` to `http://127.0.0.1:8090`, the console listener of
a bridge in console mode; `BRIDGE_CONSOLE_URL` points it elsewhere.
