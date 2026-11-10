# Run report template

Copy this to `sdks/<lang>/RUN.md` and fill every section. Keep the headings as they are: the phase 6
check and the planner read them. A section with nothing in it says "None." rather than disappearing.

```markdown
# <Language> SDK: run report

## Prompt version

- Prompt: <output of `git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run>
- Model: <the model that ran, as precisely as you know it>
- Date: <yyyy-mm-dd>
- Input: language <name>; runtime <given, or "chosen: ...">; minimum version <given, or "chosen: ...">;
  package manager <given, or "chosen: ...">
- Toolchain: <each tool and its exact version, as `mise.toml` pins them and as their version commands print>

## Results

- Vectors: <n> passed of <n>; bucket pins: <n> passed of <n> (`bin/test`)
- Conformance: <the harness's summary line, verbatim>
- Capabilities advertised: <none, or the list>
- Native tests: <n> passed of <n>

## Decisions

Choices the prompt left open on purpose. One line each: what was chosen, over what, and why.

1. <Lint and format: X over Y, because ...>

## Guesses

Points where the prompt or a document should have said and did not. Each: what was unclear, where
(document and section), and what this run assumed.

1. <What was unclear>. Where: <document, section>. Assumed: <what the run did>.

## Stuck points

Anything that needed outside help, or a blocker in the contract, the harness, a scenario or a vector
(quote both sides). In a good run: None.

## Proposed prompt changes

Concrete wording, each tied to a guess or a stuck point above by its number, and aimed at one file:
`PROMPT.md`, `CHECKLIST.md` or `language-notes/<lang>.md`.

1. For guess 1, in `PROMPT.md` phase <n>, after "<existing words>", add: "<new words>".

## Checklist

Every line of `CHECKLIST.md`, in its order, with the evidence: a test name, a command and what it printed,
or a file and line. A line that does not apply says why.

- [x] <the line, shortened>: <evidence>
```
