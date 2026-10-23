package connections

import (
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

// Hyperliquid L1 action signing, ported from the official Python SDK
// (hyperliquid-python-sdk, hyperliquid/utils/signing.py: action_hash,
// construct_phantom_agent, l1_payload, sign_l1_action, float_to_wire). Only
// L1 actions are implemented. The user-signed actions (withdraw3, usdSend,
// spotSend, sendAsset and the rest) have no code path here: an API wallet
// cannot produce them and Mewla never holds the main wallet key.

// hlKV and hlMap are an ordered msgpack map. The exchange hashes the packed
// bytes, so key order is part of the signature and a Go map cannot be used.
type hlKV struct {
	K string
	V any
}
type hlMap []hlKV

func keccak256(parts ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

// msgpackEncode packs the value kinds an exchange action uses, choosing the
// smallest encoding the way msgpack-python does. Integers are unsigned.
func msgpackEncode(v any) ([]byte, error) {
	var out []byte
	err := msgpackAppend(&out, v)
	return out, err
}

func msgpackAppend(out *[]byte, v any) error {
	switch x := v.(type) {
	case nil:
		*out = append(*out, 0xc0)
	case bool:
		if x {
			*out = append(*out, 0xc3)
		} else {
			*out = append(*out, 0xc2)
		}
	case int:
		if x < 0 {
			return errors.New("negative integers are not used in exchange actions")
		}
		return msgpackAppend(out, uint64(x))
	case uint64:
		switch {
		case x < 0x80:
			*out = append(*out, byte(x))
		case x <= 0xff:
			*out = append(*out, 0xcc, byte(x))
		case x <= 0xffff:
			*out = binary.BigEndian.AppendUint16(append(*out, 0xcd), uint16(x))
		case x <= 0xffffffff:
			*out = binary.BigEndian.AppendUint32(append(*out, 0xce), uint32(x))
		default:
			*out = binary.BigEndian.AppendUint64(append(*out, 0xcf), x)
		}
	case string:
		n := len(x)
		switch {
		case n < 32:
			*out = append(*out, 0xa0|byte(n))
		case n <= 0xff:
			*out = append(*out, 0xd9, byte(n))
		case n <= 0xffff:
			*out = binary.BigEndian.AppendUint16(append(*out, 0xda), uint16(n))
		default:
			*out = binary.BigEndian.AppendUint32(append(*out, 0xdb), uint32(n))
		}
		*out = append(*out, x...)
	case []any:
		n := len(x)
		switch {
		case n < 16:
			*out = append(*out, 0x90|byte(n))
		case n <= 0xffff:
			*out = binary.BigEndian.AppendUint16(append(*out, 0xdc), uint16(n))
		default:
			*out = binary.BigEndian.AppendUint32(append(*out, 0xdd), uint32(n))
		}
		for _, e := range x {
			if err := msgpackAppend(out, e); err != nil {
				return err
			}
		}
	case hlMap:
		n := len(x)
		switch {
		case n < 16:
			*out = append(*out, 0x80|byte(n))
		case n <= 0xffff:
			*out = binary.BigEndian.AppendUint16(append(*out, 0xde), uint16(n))
		default:
			*out = binary.BigEndian.AppendUint32(append(*out, 0xdf), uint32(n))
		}
		for _, kv := range x {
			if err := msgpackAppend(out, kv.K); err != nil {
				return err
			}
			if err := msgpackAppend(out, kv.V); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported action value %T", v)
	}
	return nil
}

// hlActionHash is the SDK's action_hash without a vault address or expiry:
// msgpack(action) + 8-byte big-endian nonce + 0x00, hashed with keccak256.
func hlActionHash(action hlMap, nonce uint64) ([]byte, error) {
	packed, err := msgpackEncode(action)
	if err != nil {
		return nil, err
	}
	packed = binary.BigEndian.AppendUint64(packed, nonce)
	packed = append(packed, 0x00)
	return keccak256(packed), nil
}

func hlWord(n uint64) []byte {
	w := make([]byte, 32)
	binary.BigEndian.PutUint64(w[24:], n)
	return w
}

// hlL1Digest is the EIP-712 digest of the phantom Agent{source, connectionId}
// under the "Exchange" domain, chain id 1337. Source "a" is mainnet and "b"
// is testnet, so a signature for one network is never valid on the other.
func hlL1Digest(connectionID []byte, mainnet bool) []byte {
	source := "b"
	if mainnet {
		source = "a"
	}
	domainType := keccak256([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	domain := keccak256(domainType, keccak256([]byte("Exchange")), keccak256([]byte("1")), hlWord(1337), make([]byte, 32))
	agentType := keccak256([]byte("Agent(string source,bytes32 connectionId)"))
	agent := keccak256(agentType, keccak256([]byte(source)), connectionID)
	return keccak256([]byte{0x19, 0x01}, domain, agent)
}

type hlSignature struct {
	R string `json:"r"`
	S string `json:"s"`
	V int    `json:"v"`
}

// hlHex renders an integer like Python's to_hex: minimal digits, no padding.
func hlHex(b []byte) string { return "0x" + new(big.Int).SetBytes(b).Text(16) }

func hlSignL1(key *secp256k1.PrivateKey, action hlMap, nonce uint64, mainnet bool) (hlSignature, error) {
	hash, err := hlActionHash(action, nonce)
	if err != nil {
		return hlSignature{}, err
	}
	compact := ecdsa.SignCompact(key, hlL1Digest(hash, mainnet), false)
	return hlSignature{R: hlHex(compact[1:33]), S: hlHex(compact[33:65]), V: int(compact[0])}, nil
}

// hlAddress is the Ethereum address of a key: the last 20 bytes of the
// keccak256 of the uncompressed public key, lowercase.
func hlAddress(key *secp256k1.PrivateKey) string {
	return hlAddressOfPub(key.PubKey().SerializeUncompressed())
}

func hlAddressOfPub(uncompressed []byte) string {
	return "0x" + hex.EncodeToString(keccak256(uncompressed[1:])[12:])
}

func hlParseKey(raw string) (*secp256k1.PrivateKey, error) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "0x")
	b, err := hex.DecodeString(raw)
	if err != nil || len(b) != 32 {
		return nil, errors.New("an API wallet private key is 64 hexadecimal characters, with or without 0x")
	}
	var n secp256k1.ModNScalar
	if overflow := n.SetByteSlice(b); overflow || n.IsZero() {
		return nil, errors.New("not a valid private key")
	}
	return secp256k1.PrivKeyFromBytes(b), nil
}

func hlSameAddress(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(a)), []byte(strings.ToLower(b))) == 1
}

func hlValidAddress(s string) bool {
	if len(s) != 42 || !strings.HasPrefix(s, "0x") {
		return false
	}
	_, err := hex.DecodeString(s[2:])
	return err == nil
}

// hlWireDecimal is the SDK's float_to_wire for a decimal string: at most 8
// decimals, no trailing zeros, no exponent. It refuses a value it would have
// to round instead of changing it.
func hlWireDecimal(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, "eE+/ ") {
		return "", fmt.Errorf("%q is not a plain decimal number", s)
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return "", fmt.Errorf("%q is not a plain decimal number", s)
	}
	scaled := new(big.Rat).Mul(r, big.NewRat(100000000, 1))
	if !scaled.IsInt() {
		return "", fmt.Errorf("%s has more than 8 decimal places", s)
	}
	if r.Sign() == 0 {
		return "0", nil
	}
	out := r.FloatString(8)
	if strings.Contains(out, ".") {
		out = strings.TrimRight(strings.TrimRight(out, "0"), ".")
	}
	return out, nil
}
