export interface SseEvent {
  event: string;
  data: string;
}

/**
 * An incremental Server-Sent Events parser: feed it text as it arrives, in any chunking, and it
 * returns the events completed so far. Comments (the server's keep-alives) and fields other than
 * `event` and `data` are dropped.
 */
export class SseParser {
  private buffer = "";

  feed(text: string): SseEvent[] {
    // A CR at the end may be half of a CRLF split across chunks: wait for the next one.
    const joined = this.buffer + text;
    const held = joined.endsWith("\r") ? "\r" : "";
    const lines = joined.slice(0, joined.length - held.length).replace(/\r\n?/g, "\n");
    const blocks = lines.split("\n\n");
    this.buffer = (blocks.pop() ?? "") + held;
    return blocks.flatMap((block) => {
      const event = parseBlock(block);
      return event ? [event] : [];
    });
  }
}

function parseBlock(block: string): SseEvent | null {
  let event = "message";
  const data: string[] = [];
  for (const line of block.split("\n")) {
    if (line === "" || line.startsWith(":")) continue;
    const colon = line.indexOf(":");
    const field = colon === -1 ? line : line.slice(0, colon);
    const raw = colon === -1 ? "" : line.slice(colon + 1);
    const value = raw.startsWith(" ") ? raw.slice(1) : raw;
    if (field === "event") event = value;
    else if (field === "data") data.push(value);
  }
  return data.length === 0 ? null : { event, data: data.join("\n") };
}
