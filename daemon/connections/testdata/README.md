# Hyperliquid signing vectors

`hyperliquid_vectors.json` holds action hashes and L1 signatures produced by the
official Python SDK (`hyperliquid-dex/hyperliquid-python-sdk`, `sign_l1_action`,
`action_hash`, `float_to_wire`) for the SDK's published test key, which holds
nothing. `hyperliquid_test.go` checks that the Go signer reproduces every byte.

The first three cases are the SDK's own regression vectors (`tests/signing_test.py`).

Regenerate with the SDK installed in a scratch virtual environment, outside this
repository, so no interpreter loads code from here:

    python -I hyperliquid_gen.py > hyperliquid_vectors.json
