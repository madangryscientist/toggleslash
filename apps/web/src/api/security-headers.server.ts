/** A fresh value for each response: base64 of 16 random bytes, which CSP allows in a nonce. */
export function createNonce(): string {
  return btoa(String.fromCharCode(...crypto.getRandomValues(new Uint8Array(16))));
}

/**
 * The policy. `script-src` and `style-src` take the nonce TanStack Start puts on each inline script and style it
 * writes (hydration and the dehydrated router state), so nothing inline runs that the server did not write.
 * `default-src` stays 'self'.
 */
function contentSecurityPolicy(nonce: string): string {
  return [
    "default-src 'self'",
    `script-src 'self' 'nonce-${nonce}'`,
    `style-src 'self' 'nonce-${nonce}'`,
    "frame-ancestors 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    "object-src 'none'",
  ].join("; ");
}

/**
 * Puts the headers on whatever `serve` answers, a redirect, the sign-in page and the gate's own responses included.
 * The build's hashed assets never get here: the assets layer answers them before the Worker runs (`public/_headers`).
 */
export async function withSecurityHeaders(
  nonce: string,
  serve: () => Response | Promise<Response>,
): Promise<Response> {
  const response = await serve();
  const headers = new Headers(response.headers);
  headers.set("x-content-type-options", "nosniff");
  headers.set("referrer-policy", "no-referrer");
  headers.set("x-frame-options", "DENY");
  headers.set("cache-control", "no-store");
  headers.set("content-security-policy", contentSecurityPolicy(nonce));
  return new Response(response.body, {
    status: response.status,
    statusText: response.statusText,
    headers,
  });
}
