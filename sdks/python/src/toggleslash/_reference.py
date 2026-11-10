"""An attribute reference: a name that starts with `/`."""

from __future__ import annotations


def parse_reference(text: str) -> list[str] | None:
    components: list[str] = []
    for part in text[1:].split("/"):
        if part == "":
            return None
        decoded: list[str] = []
        i = 0
        while i < len(part):
            char = part[i]
            if char != "~":
                decoded.append(char)
                i += 1
                continue
            nxt = part[i + 1] if i + 1 < len(part) else ""
            if nxt == "0":
                decoded.append("~")
            elif nxt == "1":
                decoded.append("/")
            else:
                return None
            i += 2
        components.append("".join(decoded))
    return components
