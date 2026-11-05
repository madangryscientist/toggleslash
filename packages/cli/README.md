# featureslash

The FeatureSlash command line: manage projects, environments, flags and SDK keys on a FeatureSlash
server from the terminal, over the admin API (`/api/v1`). It is not published yet; from a checkout, run
it with `bun run featureslash <command>`.

## Connect

The CLI needs the server's URL and the **admin token** (the server's `FEATURESLASH_ADMIN_TOKEN`).

```bash
featureslash config set-url https://flags.example.com
featureslash config set-token < ./admin-token   # the token on stdin, so it stays out of your shell history
```

| Setting | Highest first |
| --- | --- |
| URL | `--url <url>` (any command that calls the server), `FEATURESLASH_URL`, the config file |
| Token | `FEATURESLASH_TOKEN`, the config file |

- An environment variable wins over the config file (one that is set but empty counts as unset).
- The file is `$XDG_CONFIG_HOME/featureslash/config.json` (`~/.config/featureslash/config.json` when
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
featureslash config set-url <url>                  save the server URL in the config file
featureslash config set-token                      save the admin token (mode 600), read from stdin

featureslash projects list                         list projects
featureslash projects create <key> [--name <name>] create a project (the name defaults to the key)

featureslash envs list --project <project>                          list a project's environments
featureslash envs create <key> --project <project> [--name <name>]  create an environment

featureslash flags list --project <project> [--tag <tag>] [--archived false|true|all]
featureslash flags show <flag> --project <project> [--env <env>]    with --env, also its targeting there
featureslash flags create <key> --project <project> [--name <name>] [--variations <json>] [--description <text>]
                          [--temporary] [--owner <owner>] [--tag <tag>]... [--off-variation <n>] [--fallthrough-variation <n>]
featureslash flags archive <flag> --project <project>
featureslash flags on <flag> --project <project> --env <env>        turn a flag on in one environment
featureslash flags off <flag> --project <project> --env <env>       turn a flag off in one environment
featureslash flags history <flag> --project <project> [--env <env>] [--since <time>] [--limit <1-200>] [--cursor <cursor>]

featureslash keys create --project <project> --env <env> --kind server|client --name <name>
featureslash keys list --project <project> --env <env>
featureslash keys revoke <id> --project <project> --env <env>
```

`featureslash --help` prints the same list; `--version` prints the version.

- **Flags** are created with `[true, false]` as their variations (a boolean flag) unless `--variations` gives
  a JSON array (`--variations '["control","a","b"]'`). `flags on` and `flags off` use the server's `toggle`:
  they change `on` and nothing else, so they cannot undo someone else's targeting edit. `flags history`
  prints a page, newest first; when there is another page it ends with `more: add --cursor <cursor>`.
- **SDK keys** (`keys create`) print the plain key **once**. The server keeps only a hash and a prefix, so
  `keys list` shows prefixes and `keys revoke` takes the id from that list.
- This is the admin surface only. Targeting rules, segments and usage are not in the CLI yet.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | success |
| `1` | the server or the network failed: it could not be reached, it answered a 4xx or 5xx (including a rejected token, 401), or its reply was not what the API documents |
| `2` | a usage error: an unknown command, a missing or invalid argument or option, or no URL or token configured |

A failure prints one line on stderr, `featureslash: ...`, with the status and the server's own error
code and message (`the server answered 404 not_found: No such flag.`). Output goes through
`process.stdout`; errors and the key-shown-once reminder through `process.stderr`.

## Tests

`bun run test` (Vitest under Bun) runs every command against a fake HTTP layer, passed in as `fetch`;
nothing opens a socket. The fake is `test/fake-server.ts`.
