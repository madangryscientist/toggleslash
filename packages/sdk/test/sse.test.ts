import { describe, expect, it } from "vitest";
import { SseParser } from "../src/sse.ts";

describe("SseParser", () => {
  it("reads events the server writes, and skips its keep-alive comments", () => {
    const parser = new SseParser();
    const text = 'event: connected\ndata: "v1"\n\n: keep-alive\n\nevent: changed\ndata: "v2"\n\n';
    expect(parser.feed(text)).toEqual([
      { event: "connected", data: '"v1"' },
      { event: "changed", data: '"v2"' },
    ]);
  });

  it("holds an event until its blank line arrives, however the text is cut", () => {
    const parser = new SseParser();
    const parts = ["event: chan", "ged\nda", 'ta: "v2"\n', "\nevent: changed\ndata: ", '"v3"\n\n'];
    expect(parts.flatMap((part) => parser.feed(part))).toEqual([
      { event: "changed", data: '"v2"' },
      { event: "changed", data: '"v3"' },
    ]);
  });

  it("treats CRLF and a CRLF split between chunks as line ends", () => {
    const parser = new SseParser();
    const parts = ["event: changed\r\ndata: 1\r", "\n\r", "\n"];
    expect(parts.flatMap((part) => parser.feed(part))).toEqual([{ event: "changed", data: "1" }]);
  });

  it("joins data lines, defaults the event name and drops an event with no data", () => {
    const parser = new SseParser();
    expect(parser.feed("data: a\ndata: b\n\nevent: lonely\n\n")).toEqual([
      { event: "message", data: "a\nb" },
    ]);
  });
});
