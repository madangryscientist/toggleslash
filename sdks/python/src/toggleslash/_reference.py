"""An attribute reference: a name that starts with `/` (docs/evaluation.md, "Reading the attribute")."""

from __future__ import annotations


def parse_reference(text: str) -> list[str] | None:
    """The components of `text`, or None when a component is empty or a `~` is not `~0` or `~1`."""
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
            following = part[i + 1] if i + 1 < len(part) else ""
            if following == "0":
                decoded.append("~")
            elif following == "1":
                decoded.append("/")
            else:
                return None
            i += 2
        components.append("".join(decoded))
    return components
