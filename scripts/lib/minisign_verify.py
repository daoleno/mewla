#!/usr/bin/env python3
"""Verify a minisign signature without the minisign binary.

Usage: minisign_verify.py PUBKEY_BASE64 FILE [FILE.minisig]

Checks the file signature (pre-hashed "ED" BLAKE2b-512 or legacy "Ed") and the
signature over the trusted comment. Exits non-zero with a message on failure.
Needs the `cryptography` Python package (Ed25519); `available()` says whether
it is installed so callers can fall back to another check.
"""
import base64
import hashlib
import sys

try:
    from cryptography.exceptions import InvalidSignature
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
except ImportError:  # pragma: no cover - depends on the host
    InvalidSignature = None
    Ed25519PublicKey = None


class SignatureError(Exception):
    pass


def available() -> bool:
    return Ed25519PublicKey is not None


def verify(pubkey_b64: str, path: str, sig_path: str) -> str:
    """Return the trusted comment when `path` is signed by `pubkey_b64`."""
    if not available():
        raise SignatureError("python 'cryptography' is required to verify minisign signatures")

    pub = base64.b64decode(pubkey_b64)
    if len(pub) != 42 or pub[:2] != b"Ed":
        raise SignatureError("malformed public key")
    key_id, pub_raw = pub[2:10], pub[10:]

    with open(sig_path, "r", encoding="utf-8") as f:
        lines = f.read().splitlines()
    if len(lines) < 4 or not lines[2].startswith("trusted comment: "):
        raise SignatureError("malformed signature file")
    sig = base64.b64decode(lines[1])
    trusted_comment = lines[2][len("trusted comment: "):]
    global_sig = base64.b64decode(lines[3])
    if len(sig) != 74 or len(global_sig) != 64:
        raise SignatureError("malformed signature length")
    alg, sig_key_id, sig_raw = sig[:2], sig[2:10], sig[10:]
    if sig_key_id != key_id:
        raise SignatureError("signature was made with a different key id")

    with open(path, "rb") as f:
        data = f.read()
    if alg == b"ED":
        data = hashlib.blake2b(data, digest_size=64).digest()
    elif alg != b"Ed":
        raise SignatureError(f"unknown signature algorithm {alg!r}")

    verifier = Ed25519PublicKey.from_public_bytes(pub_raw)
    try:
        verifier.verify(sig_raw, data)
        verifier.verify(global_sig, sig_raw + trusted_comment.encode("utf-8"))
    except InvalidSignature:
        raise SignatureError(f"bad signature for {path}") from None
    return trusted_comment


def main() -> None:
    if len(sys.argv) not in (3, 4):
        sys.exit(__doc__)
    pubkey_b64, path = sys.argv[1], sys.argv[2]
    sig_path = sys.argv[3] if len(sys.argv) == 4 else path + ".minisig"
    try:
        comment = verify(pubkey_b64, path, sig_path)
    except SignatureError as exc:
        sys.exit(f"error: minisign verification failed: {exc}")
    print(f"minisign OK: {path} ({comment})")


if __name__ == "__main__":
    main()
