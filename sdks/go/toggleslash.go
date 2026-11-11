// Package toggleslash is the Go server SDK for ToggleSlash: it keeps a flag ruleset current from the server
// and evaluates flags locally, so an evaluation is a function call with no network in it.
//
//	client := toggleslash.New("https://flags.example", sdkKey)
//	defer client.Close()
//	client.Ready()
//	on := client.BoolVariation("new-checkout", map[string]any{"key": "user-1"}, false)
//
// The behaviour is docs/sdk-contract.md. No method panics or returns an error: a flag that cannot be
// evaluated gives the fallback you passed, and the ...Detail methods say why. Failures the SDK absorbs go to
// the WithOnError callback. Every method is safe to call from any goroutine.
//
// A context is a toggleslash.Value, a map[string]any, JSON text as []byte or json.RawMessage, or anything
// json.Marshal can write. It must be a JSON object with a string "key" (or a multi-context, see
// docs/evaluation.md); anything else is ERROR INVALID_CONTEXT.
package toggleslash
