// Package schema embeds the JSON Schema for spmeta's JSON output so tests in
// other packages can validate against it.
package schema

import _ "embed"

// JSON is the JSON Schema (draft 2020-12) for one NowPlaying object.
//
//go:embed spmeta.schema.json
var JSON []byte
