import { chmodSync, mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { UsageError } from "./errors.ts";
import { isObject } from "./render.ts";

/** What `config set-url` and `config set-token` keep. Both optional: either can come from the environment instead. */
export interface StoredConfig {
  url?: string;
  token?: string;
}

/** The slice of the process the config needs, so a test can point it at a temp dir. */
export interface ConfigEnv {
  env: Readonly<Record<string, string | undefined>>;
  home: string;
}

/** `$XDG_CONFIG_HOME/toggleslash/config.json`, falling back to `~/.config` (an empty or relative XDG value is ignored, as the spec says). */
export function configPath({ env, home }: ConfigEnv): string {
  const xdg = env["XDG_CONFIG_HOME"];
  const base = xdg !== undefined && xdg.startsWith("/") ? xdg : join(home, ".config");
  return join(base, "toggleslash", "config.json");
}

/** Reads the file; a missing one is `{}`, a broken one is a usage error that names the path (never its contents). */
export function readConfig(path: string): StoredConfig {
  let text: string;
  try {
    text = readFileSync(path, "utf8");
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === "ENOENT") return {};
    throw new UsageError(`cannot read the config file ${path}.`);
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    throw new UsageError(`the config file ${path} is not valid JSON; fix or delete it.`);
  }
  if (!isObject(parsed)) {
    throw new UsageError(`the config file ${path} must hold a JSON object; fix or delete it.`);
  }
  const stored: StoredConfig = {};
  if (typeof parsed["url"] === "string") stored.url = parsed["url"];
  if (typeof parsed["token"] === "string") stored.token = parsed["token"];
  return stored;
}

/**
 * Writes the file with mode 600 in a directory of mode 700. It goes to a sibling file created 600 and is
 * renamed over the target, so the token is never in a file anyone else can read, not even for a moment.
 */
export function writeConfig(path: string, config: StoredConfig): void {
  const dir = join(path, "..");
  mkdirSync(dir, { recursive: true, mode: 0o700 });
  const scratch = `${path}.${process.pid}.tmp`;
  writeFileSync(scratch, `${JSON.stringify(config, null, 2)}\n`, { mode: 0o600 });
  chmodSync(scratch, 0o600);
  renameSync(scratch, path);
}

/** `http(s)://host[:port][/prefix]` without a trailing slash, query, fragment or credentials. */
export function normaliseUrl(raw: string): string {
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    throw new UsageError(`"${raw}" is not a URL. Use something like https://flags.example.com.`);
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new UsageError("the server URL must start with http:// or https://.");
  }
  if (url.username !== "" || url.password !== "") {
    throw new UsageError(
      "the server URL must not carry a user name or password; the token is set separately.",
    );
  }
  if (url.search !== "" || url.hash !== "") {
    throw new UsageError("the server URL must not have a query or a fragment.");
  }
  return `${url.origin}${url.pathname.replace(/\/+$/, "")}`;
}

export interface Connection {
  url: string;
  token: string;
}

function present(value: string | undefined): string | undefined {
  return value !== undefined && value.trim() !== "" ? value.trim() : undefined;
}

/**
 * The server URL and admin token. Precedence, highest first: `--url` (the URL only), the environment
 * (`TOGGLESLASH_URL`, `TOGGLESLASH_TOKEN`), the config file. The file is not read when the environment
 * and the flag already answer everything, so a broken file does not break a run that never needed it.
 */
export function resolveConnection(where: ConfigEnv, urlFlag: string | undefined): Connection {
  let url = present(urlFlag) ?? present(where.env["TOGGLESLASH_URL"]);
  let token = present(where.env["TOGGLESLASH_TOKEN"]);
  if (url === undefined || token === undefined) {
    const stored = readConfig(configPath(where));
    url ??= present(stored.url);
    token ??= present(stored.token);
  }
  if (url === undefined) {
    throw new UsageError(
      "no server URL. Run `toggleslash config set-url <url>`, set TOGGLESLASH_URL, or pass --url.",
    );
  }
  if (token === undefined) {
    throw new UsageError(
      "no admin token. Run `toggleslash config set-token` (the token on stdin), or set TOGGLESLASH_TOKEN.",
    );
  }
  return { url: normaliseUrl(url), token };
}
