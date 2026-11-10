"""Attribute references: ``/a/b~1c`` (evaluation.md, "Reading the attribute")."""

from __future__ import annotations


def parse_reference(name: str) -> tuple[str, ...] | None:
    """Split a name that starts with ``/`` into its decoded components, or ``None`` if it is invalid."""
    parts = name[1:].split("/")
    out = []
    for part in parts:
        if part == "":
            return None
        decoded = []
        i = 0
        n = len(part)
        while i < n:
            ch = part[i]
            if ch != "~":
                decoded.append(ch)
                i += 1
                continue
            if i + 1 >= n:
                return None
            nxt = part[i + 1]
            if nxt == "0":
                decoded.append("~")
            elif nxt == "1":
                decoded.append("/")
            else:
                return None
            i += 2
        out.append("".join(decoded))
    return tuple(out)


def attribute_path(name: str) -> tuple[str, ...] | None:
    """The components an attribute name reads, or ``None`` if the name is not valid.

    A name that does not start with ``/`` is one literal component.
    """
    if not isinstance(name, str) or name == "":
        return None
    if name.startswith("/"):
        return parse_reference(name)
    return (name,)
