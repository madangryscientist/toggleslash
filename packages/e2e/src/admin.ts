/** The admin API for what the CLI does not cover yet (targeting rules, segments): plain `fetch`, loopback only. */
export function createAdmin(options: { url: string; token: string }) {
  async function call(method: string, path: string, body?: unknown): Promise<unknown> {
    const response = await fetch(`${options.url}${path}`, {
      method,
      headers: {
        Authorization: `Bearer ${options.token}`,
        ...(body === undefined ? {} : { "Content-Type": "application/json" }),
      },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      redirect: "manual",
    });
    const text = await response.text();
    if (!response.ok) throw new Error(`${method} ${path} answered ${response.status}: ${text}`);
    return text === "" ? null : JSON.parse(text);
  }
  return { get: (path: string) => call("GET", path), send: call };
}

export type Admin = ReturnType<typeof createAdmin>;
