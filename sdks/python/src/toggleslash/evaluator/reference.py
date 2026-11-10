"""An attribute reference: a name that starts with ``/``."""

from __future__ import annotations


def parse_reference(text: str) -> list[str] | None:
    """The components of a reference, or None when it is invalid.

    Drop the leading ``/``, split on ``/``, decode ``~1`` as ``/`` and ``~0`` as ``~`` in one pass.
    A component that is empty, or a ``~`` followed by anything but ``0`` or ``1`` or ending the
    component, makes the reference invalid.
    """
    components: list[str] = []
    for part in text[1:].split("/"):
        if part == "":
            return None
        decoded: list[str] = []
        index = 0
        while index < len(part):
            char = part[index]
            if char != "~":
                decoded.append(char)
                index += 1
                continue
            follow = part[index + 1 : index + 2]
            if follow == "0":
                decoded.append("~")
            elif follow == "1":
                decoded.append("/")
            else:
                return None
            index += 2
        components.append("".join(decoded))
    return components
