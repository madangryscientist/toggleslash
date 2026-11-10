# Server SDKs

Each directory here is a ToggleSlash server SDK in one language, built from
[`docs/sdk-authoring/PROMPT.md`](../docs/sdk-authoring/PROMPT.md) and checked against the
[SDK contract](../docs/sdk-contract.md). The TypeScript SDK is the reference and lives in
[`packages/sdk`](../packages/sdk), inside the Nx gate; nothing here is. No SDK is published.

## Layout

```
sdks/
  README.md                 this page
  <lang>/                   python, go, java, csharp, ruby; any other language by its lowercase name
    mise.toml               the pinned toolchain
    DESIGN.md               the mapping table and the design choices
    README.md               quickstart, guarantees, how to run the checks
    RUN.md                  the report of the run that built it
    bin/lint                the native lint and format check
    bin/test                the native tests, every evaluation vector included
    bin/test-service        starts the conformance test service
    test-service/           the test service and its README
    ...                     the language's own layout for the SDK and its tests
```

Each SDK also has `.github/workflows/sdk-<lang>.yml`, which runs on a change to that SDK, the fixtures,
the contract, the test-service protocol or the harness.

## Running an SDK's checks

From the repository root, with the toolchain in `sdks/<lang>/mise.toml` installed
(`(cd sdks/<lang> && mise trust && mise install)`) and `bun install` run once:

```sh
sdks/<lang>/bin/lint
sdks/<lang>/bin/test
bun run conformance -- --service sdks/<lang>/bin/test-service
```

## Why these are not Nx projects

Not every machine has every toolchain, so `sdks/` is in `.nxignore` and `bun run test` does not run an SDK
here. Each SDK's own workflow does. A change to the contract, the harness, the scenarios or the vectors
must keep every SDK here green; see "SDKs in `sdks/`" in [`AGENTS.md`](../AGENTS.md).

## Adding a language

Give a fresh builder `docs/sdk-authoring/PROMPT.md`, the language name and any of the optional inputs it
lists, and nothing else. It builds
`sdks/<lang>` and the workflow, and writes `RUN.md`. Its proposed prompt changes are folded into
`docs/sdk-authoring/` in a separate pull request before the next language starts.
