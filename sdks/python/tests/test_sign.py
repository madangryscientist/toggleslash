"""`sign_context` (contract 14) on every message in fixtures/signing, a round trip and the refusals."""

from __future__ import annotations

import base64
import json
import pathlib
import sys
import unittest
from unittest import mock

from cryptography.exceptions import InvalidSignature
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.hazmat.primitives.asymmetric.utils import encode_dss_signature

import toggleslash
from toggleslash._sign import context_message

MESSAGES = (
    pathlib.Path(__file__).resolve().parent.parent.parent.parent
    / "fixtures"
    / "signing"
    / "messages.json"
)


def make_key(curve: ec.EllipticCurve) -> tuple[str, ec.EllipticCurvePublicKey]:
    """A key made for this run, never written anywhere: the PKCS#8 PEM and its public half."""
    key = ec.generate_private_key(curve)
    pem = key.private_bytes(
        serialization.Encoding.PEM,
        serialization.PrivateFormat.PKCS8,
        serialization.NoEncryption(),
    ).decode("ascii")
    return pem, key.public_key()


def verifies(public: ec.EllipticCurvePublicKey, signature: str, message: bytes) -> bool:
    raw = base64.urlsafe_b64decode(signature + "==")
    der = encode_dss_signature(int.from_bytes(raw[:32], "big"), int.from_bytes(raw[32:], "big"))
    try:
        public.verify(der, message, ec.ECDSA(hashes.SHA256()))
    except InvalidSignature:
        return False
    return True


def assert_shape(test: unittest.TestCase, signature: str) -> None:
    test.assertEqual(len(signature), 86)
    test.assertNotIn("=", signature)
    test.assertTrue(
        set(signature) <= set("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_")
    )
    test.assertEqual(len(base64.urlsafe_b64decode(signature + "==")), 64)


class SignContext(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.pem, cls.public = make_key(ec.SECP256R1())
        with MESSAGES.open(encoding="utf-8") as handle:
            cls.messages = json.load(handle)["messages"]

    def test_every_fixture_message_is_the_bytes_signed_and_verifies(self) -> None:
        self.assertEqual(len(self.messages), 7)
        for entry in self.messages:
            with self.subTest(name=entry["name"]):
                message = entry["message"].encode("utf-8")
                self.assertEqual(context_message(entry["context"]), message)
                signature = toggleslash.sign_context(self.pem, entry["context"])
                assert_shape(self, signature)
                self.assertTrue(verifies(self.public, signature, message))

    def test_a_round_trip_with_a_fresh_key(self) -> None:
        pem, public = make_key(ec.SECP256R1())
        context = {"kind": "multi", "contexts": {"user": {"key": "u-9"}, "org": {"key": "o"}}}
        signature = toggleslash.sign_context(pem, context)
        assert_shape(self, signature)
        self.assertTrue(verifies(public, signature, b"3:org,1:o,4:user,3:u-9,"))
        self.assertFalse(verifies(self.public, signature, b"3:org,1:o,4:user,3:u-9,"))
        self.assertFalse(verifies(public, signature, b"4:user,3:u-9,"))

    def test_a_key_on_another_curve_is_refused(self) -> None:
        for curve in (ec.SECP384R1(), ec.SECP521R1()):
            pem, _ = make_key(curve)
            with self.subTest(curve=curve.name), self.assertRaisesRegex(ValueError, "P-256"):
                toggleslash.sign_context(pem, {"key": "user-1"})

    def test_a_key_that_is_not_a_pem_is_refused(self) -> None:
        for key in ("", "not a pem", None, 7):
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, "P-256"):
                toggleslash.sign_context(key, {"key": "user-1"})  # type: ignore[arg-type]

    def test_an_invalid_context_is_refused(self) -> None:
        for context in ({}, {"key": ""}, {"kind": "multi", "contexts": {}}, None, "user-1"):
            with self.subTest(context=context), self.assertRaisesRegex(ValueError, "context"):
                toggleslash.sign_context(self.pem, context)

    def test_a_lone_surrogate_in_a_key_is_signed_as_the_replacement_character(self) -> None:
        # As TypeScript's Buffer.from(text, "utf8") and the server write it.
        self.assertEqual(context_message({"key": "\ud800"}), b"4:user,3:\xef\xbf\xbd,")

    def test_without_cryptography_it_names_the_extra(self) -> None:
        hidden = {name: None for name in sys.modules if name.split(".")[0] == "cryptography"}
        hidden["cryptography"] = None
        with (
            mock.patch.dict(sys.modules, hidden),
            self.assertRaisesRegex(ImportError, r"toggleslash\[signing\]"),
        ):
            toggleslash.sign_context(self.pem, {"key": "user-1"})


if __name__ == "__main__":
    unittest.main()
