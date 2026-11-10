"""Attribute references: a name that starts with `/`."""

from __future__ import annotations


def parse_reference(text: str) -> list[str] | None:
    """The components of a `/a/b~1c` reference, or `None` when it is not valid.

    Drops the leading `/`, splits on `/` and decodes `~1` as `/` and `~0` as `~` in one left-to-right
    pass. `None` when a component is empty or a `~` is followed by anything but `0` or `1`.
    """
    components: list[str] = []
    for part in text[1:].split("/"):
        if part == "":
            return None
        decoded: list[str] = []
        index = 0
        length = len(part)
        while index < length:
            char = part[index]
            if char != "~":
                decoded.append(char)
                index += 1
                continue
            following = part[index + 1 : index + 2]
            if following == "0":
                decoded.append("~")
            elif following == "1":
                decoded.append("/")
            else:
                return None
            index += 2
        components.append("".join(decoded))
    return components
