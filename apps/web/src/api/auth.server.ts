/**
 * One operator, no database: the password and the signing secret come from the Worker's environment,
 * and a session is a cookie holding its expiry and an HMAC of it. Nothing here reads the environment or
 * logs, so a value can only leave through the cookie's signature.
 */
import { signInPath } from "./return-path.ts";

export const SESSION_COOKIE = "toggleslash_session";
export const SESSION_SECONDS = 12 * 60 * 60;

const MIN_PASSWORD_LENGTH = 16;
const MIN_SECRET_LENGTH = 32;

export interface AuthSecrets {
  password: string | undefined;
  sessionSecret: string | undefined;
}

/** A variable the operator has to set, and how long it must be. Never carries a value. */
interface ConfigProblem {
  variable: string;
  minimumLength: number;
}

/** What stops anyone signing in. Empty means the dashboard can be used. */
export function configProblems(secrets: AuthSecrets): ConfigProblem[] {
  const problems: ConfigProblem[] = [];
  if ((secrets.password ?? "").length < MIN_PASSWORD_LENGTH) {
    problems.push({
      variable: "TOGGLESLASH_OPERATOR_PASSWORD",
      minimumLength: MIN_PASSWORD_LENGTH,
    });
  }
  if ((secrets.sessionSecret ?? "").length < MIN_SECRET_LENGTH) {
    problems.push({ variable: "TOGGLESLASH_SESSION_SECRET", minimumLength: MIN_SECRET_LENGTH });
  }
  return problems;
}

const encoder = new TextEncoder();

function hmacKey(secret: string): Promise<CryptoKey> {
  return crypto.subtle.importKey(
    "raw",
    encoder.encode(secret),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign", "verify"],
  );
}

function hmac(key: CryptoKey, message: string): Promise<ArrayBuffer> {
  return crypto.subtle.sign("HMAC", key, encoder.encode(message));
}

/** `session:` and `password:` keep a MAC of one from ever standing in for the other. */
const sessionMessage = (expiry: string): string => `session:${expiry}`;
const passwordMessage = (password: string): string => `password:${password}`;

/** Hex has one spelling per byte string, so changing any character of a cookie changes its signature. */
function toHex(bytes: ArrayBuffer): string {
  return Array.from(new Uint8Array(bytes), (b) => b.toString(16).padStart(2, "0")).join("");
}

/** The cookie's value: the expiry in epoch milliseconds, a dot, and the signature in hex. */
export async function signSession(secret: string, nowMs: number): Promise<string> {
  const expiry = String(nowMs + SESSION_SECONDS * 1000);
  return `${expiry}.${toHex(await hmac(await hmacKey(secret), sessionMessage(expiry)))}`;
}

const SESSION_SHAPE = /^(\d{1,16})\.([0-9a-f]{64})$/;

/** `crypto.subtle.verify` compares in constant time; a string compare of the signature would not. */
export async function verifySession(
  secret: string,
  value: string | undefined,
  nowMs: number,
): Promise<boolean> {
  const match = SESSION_SHAPE.exec(value ?? "");
  if (match === null) return false;
  const [, expiry = "", signature = ""] = match;
  const bytes = Uint8Array.from(signature.match(/../g) ?? [], (pair) => Number.parseInt(pair, 16));
  const genuine = await crypto.subtle.verify(
    "HMAC",
    await hmacKey(secret),
    bytes,
    encoder.encode(sessionMessage(expiry)),
  );
  return genuine && Number(expiry) > nowMs;
}

/** Compares HMACs of both values, so the time taken says nothing about the password or its length. */
export async function passwordMatches(
  secrets: { password: string; sessionSecret: string },
  submitted: string,
): Promise<boolean> {
  const key = await hmacKey(secrets.sessionSecret);
  return crypto.subtle.verify(
    "HMAC",
    key,
    await hmac(key, passwordMessage(secrets.password)),
    encoder.encode(passwordMessage(submitted)),
  );
}

function cookieValue(request: Request): string | undefined {
  for (const part of (request.headers.get("cookie") ?? "").split(";")) {
    const [name, value] = part.trim().split("=", 2);
    if (name === SESSION_COOKIE) return value;
  }
  return undefined;
}

/** Whether this request carries a live session. A misconfigured dashboard has none, whatever it sends. */
export async function hasSession(
  request: Request,
  secrets: AuthSecrets,
  nowMs: number,
): Promise<boolean> {
  if (configProblems(secrets).length > 0) return false;
  return verifySession(secrets.sessionSecret ?? "", cookieValue(request), nowMs);
}

const LOOPBACK_HOSTS = new Set(["localhost", "127.0.0.1", "[::1]"]);

function cookieAttributes(requestUrl: string, maxAge: number): string {
  const secure = LOOPBACK_HOSTS.has(new URL(requestUrl).hostname) ? "" : "; Secure";
  return `Path=/; HttpOnly; SameSite=Lax; Max-Age=${maxAge}${secure}`;
}

export function sessionCookie(requestUrl: string, value: string): string {
  return `${SESSION_COOKIE}=${value}; ${cookieAttributes(requestUrl, SESSION_SECONDS)}`;
}

export function clearedSessionCookie(requestUrl: string): string {
  return `${SESSION_COOKIE}=; ${cookieAttributes(requestUrl, 0)}`;
}

function redirect(location: string, cookie?: string): Response {
  const headers = new Headers({ location, "cache-control": "no-store" });
  if (cookie !== undefined) headers.set("set-cookie", cookie);
  return new Response(null, { status: 303, headers });
}

/** The sign-in form's POST. Every failure answers the same way, and the password goes no further. */
async function signIn(request: Request, secrets: AuthSecrets, nowMs: number): Promise<Response> {
  const form = await request.formData().catch(() => null);
  const returnTo = form?.get("returnTo");
  const submitted = form?.get("password");
  const { password, sessionSecret } = secrets;
  const target = typeof returnTo === "string" ? returnTo : null;
  if (
    typeof submitted === "string" &&
    password !== undefined &&
    sessionSecret !== undefined &&
    configProblems(secrets).length === 0 &&
    (await passwordMatches({ password, sessionSecret }, submitted))
  ) {
    const value = await signSession(sessionSecret, nowMs);
    return redirect(signInPath.after(target), sessionCookie(request.url, value));
  }
  return redirect(signInPath.failed(target));
}

/** What the sign-in page is told by the request that renders it. */
export interface RequestContext {
  configProblems: ConfigProblem[];
}

/**
 * The Worker's front door, and only ever a redirect: the protection is `requireSession`, which runs on
 * every server function. A signed-out page request goes to /sign-in; server functions pass through so
 * that the middleware can refuse them with a status rather than a page.
 */
export async function gate(
  request: Request,
  secrets: AuthSecrets,
  nowMs: number,
  next: (context: RequestContext) => Response | Promise<Response>,
): Promise<Response> {
  const url = new URL(request.url);
  if (url.pathname.startsWith("/_serverFn/")) return next({ configProblems: [] });
  const signedIn = await hasSession(request, secrets, nowMs);
  if (url.pathname === "/sign-in") {
    if (request.method === "POST") return signIn(request, secrets, nowMs);
    if (signedIn) return redirect(signInPath.after(url.searchParams.get("returnTo")));
    return next({ configProblems: configProblems(secrets) });
  }
  if (!signedIn) return redirect(signInPath.to(`${url.pathname}${url.search}`));
  return next({ configProblems: [] });
}
