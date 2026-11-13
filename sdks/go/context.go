package toggleslash

import "github.com/madangryscientist/toggleslash/sdks/go/jsonval"

// Context is a single context or a multi context, exactly as
// docs/evaluation.md "Context" defines them, as the JSON value the evaluator
// takes. No kind means "user", and an invalid context is not an error here: it
// evaluates to ERROR INVALID_CONTEXT and the caller gets its fallback.
//
// A number the caller builds from one of Go's numeric types is
// jsonval.Number(float64(v)), which is the JSON number of that value read as
// binary64 (docs/sdk-contract.md section 3).
type Context = jsonval.Value
