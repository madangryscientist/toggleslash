# Run report template

Copy this to `sdks/<lang>/RUN.md` and fill every section. Keep the headings as they are: the phase 6b
check and the planner read them. A section with nothing in it says "None." rather than disappearing.

```markdown
# <Language> SDK: run report

## Prompt version

- Prompt: <output of `git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run>
- Model: <the model that ran, as precisely as you know it>
- Date: <yyyy-mm-dd>
- Input: language <name>; runtime <given, or "chosen: ...">; minimum version <given, or "chosen: ...">;
  package manager <given, or "chosen: ...">
- Toolchain: <each tool and its exact version, as `mise.toml` pins them and as their version commands print; then the plugins and test libraries the build file pins (a `pom.xml`, a `go.mod`), with their versions>

## Results

- Vectors: <n> passed of <n>; bucket pins: <n> passed of <n> (`bin/test`)
- Conformance: <the harness's summary line, verbatim>
- Capabilities advertised: <none, or the list>
- Native tests: <n> passed of <n> (top-level tests as the native runner counts them, in every package of the SDK, its test service included where it has tests; give the subtest or table-case count beside it; a test that loops over the vectors is one test (a framework's dynamic or parameterised test factory over the vectors counts as that one test, with its case count beside it), since the vectors have their own line above)

## Decisions

Choices the prompt left open on purpose, and points a document settles. One line each: what was chosen, over
what, and why, or the point and the file and line that settles it.

1. <Lint and format: X over Y, because ...>

## Guesses

Points where the prompt or a document should have said and did not. Each: what was unclear, where
(document and section), the sentence closest to it in that section (quoted or named) and why it does not
settle the point, and what this run assumed. A point a document settles is a Decision (with its file and line),
not a guess.

1. <What was unclear>. Where: <document, section>. Closest sentence: <quote or name; why it does not settle it>. Assumed: <what the run did>.

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

## Phase log

One entry per phase (0, 1, 2, 3a, 3b, 4, 5, 6a and 6b), written by the session that ran it (`PROMPT.md`, "How a run is split").
Each entry is all the next session gets, so write it to be read cold.

### Phase <n>: <name>

- Check: <the check the phase ran> -> <that check's last output line>
- Files written: <paths>
- Decisions: <what was chosen and why; these also go under Decisions>
- Open items: <what is unfinished or doubtful, or "None.">
- Next phase reads first: <the files, sections or entry lines the next phase must start from>
```
