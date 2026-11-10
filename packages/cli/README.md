# toggleslash

The ToggleSlash command line: manage projects, environments, flags, SDK keys and scoped API tokens on a ToggleSlash
server from the terminal, over the admin API (`/api/v1`). It is not published yet; from a checkout, run
it with `bun run toggleslash <command>`.

## Connect

The CLI needs the server's URL and the **admin token** (the server's `TOGGLESLASH_ADMIN_TOKEN`).

```bash
toggleslash config set-url https://flags.example.com
toggleslash config set-token < ./admin-token   # the token on stdin, so it stays out of your shell history
```

| Setting | Highest first |
| --- | --- |
| URL | `--url <url>` (any command that calls the server), `TOGGLESLASH_URL`, the config file |
| Token | `TOGGLESLASH_TOKEN`, the config file |

- An environment variable wins over the config file (one that is set but empty counts as unset).
- The file is `$XDG_CONFIG_HOME/toggleslash/config.json` (`~/.config/toggleslash/config.json` when
  `XDG_CONFIG_HOME` is unset). `config set-token` writes it with mode **600**, in a directory of mode 700,
  and tightens a file that was looser. `config set-url` and `config set-token` each keep the other's value.
- `config set-token` reads the token from stdin only; an argument is refused, because it would land in
  your shell history and the process list.
- There is no `--token` flag, on purpose. The token is never printed: not in output, not in an error (the
  CLI replaces it with `***` even if a server or proxy echoes it back).
- Redirects are not followed, so a redirect cannot carry the token to another host. Use `https` for any
  server that is not on your own machine: the token travels in an `Authorization` header.

## Commands

Every command that calls the server takes `--url <url>` and `--json`. `--json` prints exactly what the
server sent, pretty-printed, so a script sees the API's own shapes (`flags show --env` nests the
targeting under `config`; `keys revoke` prints `{"id": 7, "revoked": true}`). Without it you get a table
or `name: value` lines. `-p` is `--project` and `-e` is `--env`.

```
toggleslash config set-url <url>                  save the server URL in the config file
toggleslash config set-token                      save the admin token (mode 600), read from stdin

toggleslash projects list                         list projects
toggleslash projects create <key> [--name <name>] create a project (the name defaults to the key)

toggleslash envs list --project <project>                          list a project's environments
toggleslash envs create <key> --project <project> [--name <name>]  create an environment
toggleslash envs rename <key> --project <project> --name <name>    rename an environment (its key never changes)
toggleslash envs delete <key> --project <project> --yes            delete an environment (see below)

toggleslash flags list --project <project> [--tag <tag>] [--archived false|true|all]
toggleslash flags show <flag> --project <project> [--env <env>]    with --env, also its targeting there
toggleslash flags create <key> --project <project> [--name <name>] [--variations <json>] [--description <text>]
                          [--temporary] [--owner <owner>] [--tag <tag>]... [--off-variation <n>] [--fallthrough-variation <n>]
toggleslash flags archive <flag> --project <project>
toggleslash flags delete <flag> --project <project>                delete an archived flag for good (refused while another flag lists it as a prerequisite)
toggleslash flags on <flag> --project <project> --env <env>        turn a flag on in one environment
toggleslash flags off <flag> --project <project> --env <env>       turn a flag off in one environment
toggleslash flags history <flag> --project <project> [--env <env>] [--since <time>] [--limit <1-200>] [--cursor <cursor>]

toggleslash keys create --project <project> --env <env> --kind server|client --name <name>
toggleslash keys list --project <project> --env <env>
toggleslash keys revoke <id> --project <project> --env <env>

toggleslash tokens create --project <project> --name <name> --permission read|write --expires-in-days <1-365>
toggleslash tokens list --project <project>
toggleslash tokens revoke <id> --project <project>

toggleslash diagnose <flag> --project <project> --env <env> (--context '<json>' | --context-file <path>) [--etag <hash>]
toggleslash runtime --project <project> --env <env>
toggleslash rulesets show <hash> --project <project> --env <env> [--flag <flag>]
```

`toggleslash --help` prints the same list; `--version` prints the version.

- **Flags** are created with `[true, false]` as their variations (a boolean flag) unless `--variations` gives
  a JSON array (`--variations '["control","a","b"]'`). `flags on` and `flags off` use the server's `toggle`:
  they change `on` and nothing else, so they cannot undo someone else's targeting edit. `flags history`
  prints a page, newest first; when there is another page it ends with `more: add --cursor <cursor>`.
- **Environments**: `envs rename` changes the name and never the key, which audit rows, clients and `?env=` URLs use; it
  needs `--name`, and the ruleset does not change. `envs delete` deletes the environment with its flags' settings
  there, its segments, its revoked keys, its usage counts and its kept rulesets; the audit trail stays readable. There is
  no prompt, because the CLI is scripted: without `--yes` it exits 2 and sends nothing. The server refuses (exit 1, with
  its message) while the environment has any SDK key that is not revoked, so revoke them with `keys revoke` first. A
  write-scoped token may do both for its own project.
- **SDK keys** (`keys create`) print the plain key **once**. The server keeps only a hash and a prefix, so
  `keys list` shows prefixes and `keys revoke` takes the id from that list.
- **Scoped API tokens** (`tokens create`) are for Terraform and CI: one project, `read` or `write`, always with an
  expiry. Like a key, the plain token prints **once**, and `tokens list` shows prefixes, expired and revoked ones
  too. Managing them takes the root admin token. A scoped token can be used as this CLI's token for its own
  project, except for `projects create` and `tokens`: a `read` token may run `diagnose`, `runtime`, `rulesets show` and every read, and a
  `write` token everything else too, including creating and revoking the project's SDK keys.
- **`diagnose`** asks the server why a flag has the value it has for one context: it evaluates the flag in
  that environment (the admin route `POST /api/v1/projects/{project}/environments/{env}/evaluate`, so no SDK
  key) and prints `value`, `variationIndex` and `reason`, with `ruleIndex`, `flagKey` (the prerequisite that
  failed) and `code` (an error's) when the reason has them. The context is a JSON object, `{"key": "user-1",
  "attributes": {}}`, from exactly one of `--context` and `--context-file`. It exits 0 whenever the server
  answers 200, **including when the reason is an error** (`ERROR FLAG_NOT_FOUND` is an answer, not a
  failure). A diagnosis counts no usage, writes no audit event, and the server neither stores nor logs the
  context. `--etag <hash>` asks as of a past ruleset: the hash is the one `runtime` and `rulesets show` use, the
  `ETag` as 64 lowercase hex characters without its quotes (anything else is a usage error, exit 2), and the server
  evaluates the context against the ruleset it kept when it served that hash, so the value and reason are what the
  flag gave then. A hash the environment never served exits 1.
- **`runtime`** asks what the server is serving for one environment (`GET /api/v1/projects/{project}/environments/{env}/runtime`)
  and prints `etag` (the `ETag` of that environment's ruleset, quotes included, as `GET /sdk/v1/ruleset` gives a server
  key), `flagCount` (flags in that ruleset, so archived ones are not counted), `generatedAt` (the server's time) and
  `lastChange` (time, actor, action, entity type and id of the newest write that could change the ruleset, or `-`).
  It is the server's side only: it says nothing about what any SDK has loaded. A `read` token may run it, and it
  writes no audit event.
- This is the admin surface only. Targeting rules, segments and usage are not in the CLI yet.
- **`rulesets show`** answers what the flags were under a ruleset hash this environment served
  (`GET /api/v1/projects/{project}/environments/{env}/rulesets/{hash}`). The hash is the `ETag` as 64 lowercase hex
  characters without its quotes; anything else is a usage error (exit 2). The table gives the `etag`, `firstServedAt`, and
  the flags and segments with their lists as counts; `--json` has the whole thing, and no salt in either. `--flag` narrows it
  to that flag and the segments its rules name. A hash the environment never served, or a flag not in that ruleset, exits 1.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | success |
| `1` | the server or the network failed: it could not be reached, it answered a 4xx or 5xx (including a rejected token, 401), or its reply was not what the API documents |
| `2` | a usage error: an unknown command, a missing or invalid argument or option (for `diagnose`, no flag, both or neither of `--context` and `--context-file`, a file that cannot be read, or text that is not JSON), or no URL or token configured |

A failure prints one line on stderr, `toggleslash: ...`, with the status and the server's own error
code and message (`the server answered 404 not_found: No such flag.`). Output goes through
`process.stdout`; errors and the key-shown-once reminder through `process.stderr`.

## Tests

`bun run test` (Vitest under Bun) runs every command against a fake HTTP layer, passed in as `fetch`;
nothing opens a socket. The fake is `test/fake-server.ts`.
