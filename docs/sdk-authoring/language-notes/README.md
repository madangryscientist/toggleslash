# Language notes

A language note is what a run of [`PROMPT.md`](../PROMPT.md) learned about one language that the prompt
cannot say in general. Each SDK run proposes its note under "Proposed prompt changes"
in its `RUN.md`, and the planner writes or edits the note in a separate pull request before the next run.

## What a note is

- One file per language, named as its SDK directory: `python.md`, `go.md`, `java.md`, `csharp.md`,
  `ruby.md`.
- Facts about that language that cost a run a guess or a retry: how its JSON parser treats `1e999` or a
  lone surrogate, what its HTTP client's timeout bounds, how its toolchain pins in `mise.toml`, which
  lint, format and test tools a run settled on and why.
- Each fact says which run taught it (the `RUN.md`'s prompt hash and date), so a stale one can be found.
- A note MAY name tools, where the prompt names none.

## What a note is not

- Not a second contract. A note never changes a rule of [`sdk-contract.md`](../../sdk-contract.md) or
  [`evaluation.md`](../../evaluation.md); if a rule is wrong, that is a change to the contract, with its own
  pull request.
- Not a place for a fact every language needs. That goes in `PROMPT.md` or `CHECKLIST.md`.
- Not code. A note says what to watch for; the run writes the code.
