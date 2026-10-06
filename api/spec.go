// Package api holds the OpenAPI contract of the console API
// (docs/console.md).
//
// openapi-v1.yaml is the single source: the Go server in v1 and the
// console's TypeScript client are generated from it (make generate,
// npm run generate-api), and the console serves this very file.
package api

import _ "embed"

// OpenAPIV1 is openapi-v1.yaml as written.
//
//go:embed openapi-v1.yaml
var OpenAPIV1 []byte
