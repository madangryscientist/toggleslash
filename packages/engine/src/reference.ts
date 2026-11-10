/**
 * An attribute reference, to "Reading the attribute" in docs/evaluation.md: a name that starts with `/`.
 * The components are what is left after dropping the leading `/`, splitting on `/` and decoding `~1` as
 * `/` and `~0` as `~` in one left-to-right pass. Null when a component is empty or a `~` is followed by
 * anything but `0` or `1`, or ends the component. A literal name (no leading `/`) is not passed in.
 */
export function parseReference(text: string): readonly string[] | null {
  const components: string[] = [];
  for (const part of text.slice(1).split("/")) {
    if (part === "") return null;
    let decoded = "";
    for (let i = 0; i < part.length; i++) {
      const char = part.charAt(i);
      if (char !== "~") {
        decoded += char;
        continue;
      }
      const next = part.charAt(i + 1);
      if (next === "0") decoded += "~";
      else if (next === "1") decoded += "/";
      else return null;
      i++;
    }
    components.push(decoded);
  }
  return components;
}
