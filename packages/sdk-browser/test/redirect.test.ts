import { describe, expect, test } from "vitest";
import { served } from "./fake-server.ts";
import { started } from "./harness.ts";

describe("redirects", () => {
  test("does not follow a redirect: evaluate and changes both ask for manual", async () => {
    const { client, server } = await started({ a: served(true, 1) });
    const evaluates = server.requestsTo("/sdk/v1/evaluate");
    const changes = server.requestsTo("/sdk/v1/changes");
    expect(evaluates.length).toBeGreaterThan(0);
    expect(changes.length).toBeGreaterThan(0);
    for (const seen of [...evaluates, ...changes]) expect(seen.redirect).toBe("manual");
    await client.close();
  });

  test("does not follow a redirect: the usage report asks for manual too", async () => {
    const { client, server } = await started({ a: served(true, 1) });
    client.variation("a", false);
    await client.close();
    const events = server.requestsTo("/sdk/v1/events");
    expect(events).toHaveLength(1);
    expect(events[0]?.redirect).toBe("manual");
  });
});
