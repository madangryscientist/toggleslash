/**
 * Builds and packs the six npm packages for a release: `bun scripts/pack.ts [--out <dir>]`.
 *
 * In this repo every library package's `exports` point at its TypeScript source, and the apps, tests and
 * the stack rely on that. What npm gets is a staged copy instead: per library package, `tsc -p
 * tsconfig.build.json` writes `dist/` (JavaScript and `.d.ts`), and a copy under the out directory holds
 * `dist/`, `README.md`, `LICENSE`, `NOTICE` and a rewritten `package.json`: no `private`, `exports` into
 * `dist` with a `types` condition, `files`, `repository`, every `workspace:*` replaced by the lockstep
 * version (`npm publish` does not rewrite `workspace:`), and no `devDependencies`. The CLI keeps its
 * bundle (`cli:build` inlines the engine) and is staged as it is, without its `devDependencies`.
 *
 * It ends with `npm pack` of each staged copy and prints the tarball paths, in publish order (each
 * package after what it depends on). The out directory defaults to `$TMPDIR/toggleslash-pack`; its earlier
 * `staged/` and tarballs are removed first, nothing else. It publishes nothing: `release.yml` does, from these tarballs.
 */
import { execFileSync } from "node:child_process";
import {
  cpSync,
  existsSync,
  mkdirSync,
  readdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { parseArgs } from "node:util";
import { ENGINE_VERSION } from "../packages/engine/src/index.ts";

const ROOT = resolve(import.meta.dirname, "..");
const REPOSITORY = "git+https://github.com/madangryscientist/toggleslash.git";

/** Publish order: every package comes after the packages it depends on. */
const LIBRARIES = ["engine", "sdk", "sdk-browser", "sdk-otel", "sdk-openfeature"] as const;
const CLI = "cli";

type Manifest = Record<string, unknown> & {
  name: string;
  version: string;
  exports: Record<string, string> | undefined;
  dependencies: Record<string, string> | undefined;
};

const { values } = parseArgs({ options: { out: { type: "string" } } });
const out = resolve(values.out ?? join(process.env["TMPDIR"] ?? tmpdir(), "toggleslash-pack"));

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isStringRecord(value: unknown): value is Record<string, string> {
  return isRecord(value) && Object.values(value).every((entry) => typeof entry === "string");
}

function readManifest(dir: string): Manifest {
  const raw: unknown = JSON.parse(
    readFileSync(join(ROOT, "packages", dir, "package.json"), "utf8"),
  );
  if (!isRecord(raw)) throw new Error(`pack: packages/${dir}/package.json is not an object`);
  const { name, version, exports, dependencies } = raw;
  if (typeof name !== "string" || typeof version !== "string")
    throw new Error(`pack: packages/${dir}/package.json has no name or version`);
  if (exports !== undefined && !isStringRecord(exports))
    throw new Error(`pack: packages/${dir}/package.json exports are not plain paths`);
  if (dependencies !== undefined && !isStringRecord(dependencies))
    throw new Error(`pack: packages/${dir}/package.json dependencies are not plain ranges`);
  return { ...raw, name, version, exports, dependencies };
}

/** `./src/shared.ts` becomes `{ types: "./dist/shared.d.ts", default: "./dist/shared.js" }`. */
function distExport(source: string): { types: string; default: string } {
  const match = /^\.\/src\/(.+)\.ts$/.exec(source);
  if (match === null) throw new Error(`pack: export ${source} is not a ./src/*.ts path`);
  return { types: `./dist/${match[1]}.d.ts`, default: `./dist/${match[1]}.js` };
}

function pinWorkspace(deps: Record<string, string> | undefined, version: string) {
  if (deps === undefined) return undefined;
  return Object.fromEntries(
    Object.entries(deps).map(([name, range]) => [
      name,
      range.startsWith("workspace:") ? version : range,
    ]),
  );
}

function stageLicence(staged: string) {
  for (const file of ["LICENSE", "NOTICE"]) cpSync(join(ROOT, file), join(staged, file));
}

/** The checks a broken manifest would fail on npm, made before anything is packed. */
function assertPublishable(manifest: Record<string, unknown>) {
  const text = JSON.stringify(manifest);
  if ("private" in manifest) throw new Error(`pack: ${String(manifest["name"])} is still private`);
  if (text.includes("workspace:"))
    throw new Error(`pack: ${String(manifest["name"])} still has workspace:`);
}

const versions = [...new Set([...LIBRARIES, CLI].map((dir) => readManifest(dir).version))];
const [version] = versions;
if (version === undefined || versions.length !== 1)
  throw new Error(`pack: the six packages are not at one version: ${versions.join(", ")}`);
// `--version` prints the engine's own constant next to the CLI's version, so it moves with them.
if (ENGINE_VERSION !== version)
  throw new Error(`pack: ENGINE_VERSION is ${ENGINE_VERSION}, the packages are at ${version}`);

// Only what this script writes is removed, so an `--out` that names a directory with other files in it loses none.
const staging = join(out, "staged");
rmSync(staging, { recursive: true, force: true });
mkdirSync(staging, { recursive: true });
for (const file of readdirSync(out))
  if (file.startsWith("hackcove-toggleslash") && file.endsWith(".tgz")) rmSync(join(out, file));

const stagedDirs: string[] = [];

for (const dir of LIBRARIES) {
  const source = join(ROOT, "packages", dir);
  rmSync(join(source, "dist"), { recursive: true, force: true });
  // tsc prints its diagnostics on stdout; send them to stderr so a failed build says why.
  execFileSync(
    join(ROOT, "node_modules", ".bin", "tsc"),
    ["-p", join(source, "tsconfig.build.json")],
    {
      cwd: ROOT,
      stdio: ["ignore", process.stderr, "inherit"],
    },
  );

  const manifest = readManifest(dir);
  if (manifest.exports === undefined) throw new Error(`pack: ${manifest.name} has no exports`);
  const { private: _private, devDependencies: _dev, exports, dependencies, ...rest } = manifest;
  const staged: Record<string, unknown> = {
    ...rest,
    repository: { type: "git", url: REPOSITORY, directory: `packages/${dir}` },
    exports: Object.fromEntries(
      Object.entries(exports).map(([entry, path]) => [entry, distExport(path)]),
    ),
    files: ["dist", "README.md", "LICENSE", "NOTICE"],
    publishConfig: { access: "public" },
  };
  const pinned = pinWorkspace(dependencies, version);
  if (pinned !== undefined) staged["dependencies"] = pinned;
  const emitted = Object.values(exports).flatMap((path) => Object.values(distExport(path)));
  const missing = emitted.find((file) => !existsSync(join(source, file)));
  if (missing !== undefined)
    throw new Error(`pack: ${manifest.name} exports ${missing}, which tsc did not emit`);
  assertPublishable(staged);

  const target = join(staging, dir);
  mkdirSync(target, { recursive: true });
  cpSync(join(source, "dist"), join(target, "dist"), { recursive: true });
  if (!existsSync(join(source, "README.md")))
    throw new Error(`pack: ${manifest.name} has no README.md`);
  cpSync(join(source, "README.md"), join(target, "README.md"));
  stageLicence(target);
  writeFileSync(join(target, "package.json"), `${JSON.stringify(staged, null, 2)}\n`);
  stagedDirs.push(target);
}

// The CLI: always a fresh bundle, never a cache hit, so what is packed is built from this tree. moon is called
// directly, not through `scripts/moon.ts`, which fails in CI without a .NET SDK (the publish job has none, and
// `cli:build` needs none). Its output goes to stderr, so stdout holds only the tarball paths.
execFileSync(
  process.execPath,
  [join(ROOT, "node_modules", ".bin", "moon"), "run", `${CLI}:build`, "--force"],
  {
    cwd: ROOT,
    stdio: ["ignore", process.stderr, "inherit"],
  },
);
{
  const source = join(ROOT, "packages", CLI);
  const { devDependencies: _dev, ...manifest } = readManifest(CLI);
  assertPublishable(manifest);
  const target = join(staging, CLI);
  mkdirSync(target, { recursive: true });
  cpSync(join(source, "dist"), join(target, "dist"), { recursive: true });
  cpSync(join(source, "README.md"), join(target, "README.md"));
  stageLicence(target);
  writeFileSync(join(target, "package.json"), `${JSON.stringify(manifest, null, 2)}\n`);
  stagedDirs.push(target);
}

// `npm pack` prints the tarball's file name as its last line of stdout (its `--json` shape differs between npm 11 and 12).
const tarballs = stagedDirs.map((dir) => {
  const filename = execFileSync("npm", ["pack", "--pack-destination", out], {
    cwd: dir,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "inherit"],
  })
    .trim()
    .split("\n")
    .at(-1);
  if (filename === undefined || !filename.endsWith(".tgz"))
    throw new Error(`pack: npm pack in ${dir} printed no tarball`);
  return join(out, filename);
});
process.stdout.write(`${tarballs.join("\n")}\n`);
