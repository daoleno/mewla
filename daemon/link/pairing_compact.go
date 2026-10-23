package link

import (
	"bytes"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

// Version 3 layout, big-endian, after the version byte:
//
//	daemon public key 32 | enrollment token 32 | route ID 16 |
//	transport pin 32 | expires at, ms 6 | signature 64 |
//	stable label (len 1, bytes; empty means the route ID) | candidate count 1 |
//	per candidate: name (len 1, bytes) | client domain (len 1, bytes) |
//	client port 2 | flags 1 (bit 0: admission) | admission alias 16
//
// Each candidate's stable URL is wss://<label>.<domain>[:port]/ws and its
// admission URL wss://<alias hex>.<domain>[:port]/ws, as the connector builds
// them. The signature covers PairingBindingPayload of the rebuilt payload.
const (
	compactKeyBytes       = 32
	compactRouteBytes     = 16
	compactAliasBytes     = 16
	compactSignatureBytes = 64
	compactExpiryBytes    = 6
	compactMaxCandidates  = 16
	compactHasAdmission   = 1
)

var compactEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// EncodeCompactPairing encodes a version 3 payload. It fails when the
// payload's URLs do not have the connector's shape, so decoding would not
// give back the exact signed values.
func EncodeCompactPairing(payload PairingPayload) (string, error) {
	if payload.Version != PairingVersion || payload.DaemonID != "" {
		return "", errors.New("compact pairing needs a version 3 payload without a daemon ID")
	}
	if len(payload.Candidates) == 0 || len(payload.Candidates) > compactMaxCandidates {
		return "", errors.New("compact pairing needs 1 to 16 candidates")
	}
	if payload.ExpiresAtMS < 0 || payload.ExpiresAtMS >= 1<<48 {
		return "", errors.New("compact pairing expiry out of range")
	}
	var out bytes.Buffer
	out.WriteByte(PairingVersion)
	for _, field := range []struct {
		value string
		size  int
	}{
		{payload.DaemonPublicKey, compactKeyBytes},
		{payload.EnrollmentToken, compactKeyBytes},
		{payload.RouteID, compactRouteBytes},
		{payload.TransportPin, compactKeyBytes},
	} {
		raw, err := fixedHex(field.value, field.size)
		if err != nil {
			return "", err
		}
		out.Write(raw)
	}
	var expiry [8]byte
	binary.BigEndian.PutUint64(expiry[:], uint64(payload.ExpiresAtMS))
	out.Write(expiry[8-compactExpiryBytes:])
	signature, err := fixedHex(payload.Signature, compactSignatureBytes)
	if err != nil {
		return "", err
	}
	out.Write(signature)

	label := ""
	type candidateParts struct {
		domain string
		port   int
		alias  []byte
	}
	parts := make([]candidateParts, 0, len(payload.Candidates))
	for _, candidate := range payload.Candidates {
		stableHost, port, err := connectorURLHost(candidate.StableURL)
		if err != nil {
			return "", err
		}
		part := candidateParts{port: port}
		if candidate.AdmissionURL != "" {
			admissionHost, admissionPort, err := connectorURLHost(candidate.AdmissionURL)
			if err != nil {
				return "", err
			}
			aliasHex, domain, ok := strings.Cut(admissionHost, ".")
			if !ok || admissionPort != port {
				return "", errors.New("compact pairing admission URL has another shape")
			}
			alias, err := fixedHex(aliasHex, compactAliasBytes)
			if err != nil {
				return "", err
			}
			part.alias, part.domain = alias, domain
			candidateLabel, ok := strings.CutSuffix(stableHost, "."+domain)
			if !ok {
				return "", errors.New("compact pairing stable and admission domains differ")
			}
			if candidateLabel == strings.ToLower(payload.RouteID) {
				candidateLabel = ""
			}
			if label != "" && candidateLabel != label {
				return "", errors.New("compact pairing candidates have different labels")
			}
			label = candidateLabel
		}
		parts = append(parts, part)
	}
	for index, candidate := range payload.Candidates {
		if parts[index].domain != "" {
			continue
		}
		stableHost, _, _ := connectorURLHost(candidate.StableURL)
		prefix := label
		if prefix == "" {
			prefix = strings.ToLower(payload.RouteID)
		}
		domain, ok := strings.CutPrefix(stableHost, prefix+".")
		if !ok {
			return "", errors.New("compact pairing stable URL has another label")
		}
		parts[index].domain = domain
	}

	if err := writeShortString(&out, label); err != nil {
		return "", err
	}
	out.WriteByte(byte(len(payload.Candidates)))
	for index, candidate := range payload.Candidates {
		if err := writeShortString(&out, candidate.Name); err != nil {
			return "", err
		}
		if err := writeShortString(&out, parts[index].domain); err != nil {
			return "", err
		}
		var port [2]byte
		binary.BigEndian.PutUint16(port[:], uint16(parts[index].port))
		out.Write(port[:])
		if parts[index].alias == nil {
			out.WriteByte(0)
			continue
		}
		out.WriteByte(compactHasAdmission)
		out.Write(parts[index].alias)
	}

	encoded := compactEncoding.EncodeToString(out.Bytes())
	decoded, err := DecodeCompactPairing(encoded)
	if err != nil || !reflect.DeepEqual(decoded, payload) {
		return "", errors.New("compact pairing would not round-trip this payload")
	}
	return encoded, nil
}

// DecodeCompactPairing rebuilds the version 3 payload. Callers still check
// it with ValidatePairingPayload.
func DecodeCompactPairing(encoded string) (PairingPayload, error) {
	raw, err := compactEncoding.DecodeString(encoded)
	if err != nil || compactEncoding.EncodeToString(raw) != encoded {
		return PairingPayload{}, errors.New("compact pairing is not base32")
	}
	in := compactReader{data: raw}
	if in.byte() != PairingVersion {
		return PairingPayload{}, errors.New("compact pairing has another version")
	}
	payload := PairingPayload{Version: PairingVersion}
	payload.DaemonPublicKey = hex.EncodeToString(in.bytes(compactKeyBytes))
	payload.EnrollmentToken = hex.EncodeToString(in.bytes(compactKeyBytes))
	payload.RouteID = hex.EncodeToString(in.bytes(compactRouteBytes))
	payload.TransportPin = hex.EncodeToString(in.bytes(compactKeyBytes))
	var expiry [8]byte
	copy(expiry[8-compactExpiryBytes:], in.bytes(compactExpiryBytes))
	payload.ExpiresAtMS = int64(binary.BigEndian.Uint64(expiry[:]))
	payload.Signature = hex.EncodeToString(in.bytes(compactSignatureBytes))
	label := in.shortString()
	if label == "" {
		label = payload.RouteID
	}
	count := int(in.byte())
	if count == 0 || count > compactMaxCandidates {
		return PairingPayload{}, errors.New("compact pairing needs 1 to 16 candidates")
	}
	for range count {
		name := in.shortString()
		domain := in.shortString()
		port := int(binary.BigEndian.Uint16(in.bytes(2)))
		flags := in.byte()
		if name == "" || domain == "" || port == 0 || flags&^compactHasAdmission != 0 {
			return PairingPayload{}, errors.New("compact pairing has an invalid candidate")
		}
		candidate := PairingCandidate{Name: name, StableURL: candidateURL(label+"."+domain, port)}
		if flags&compactHasAdmission != 0 {
			candidate.AdmissionURL = candidateURL(hex.EncodeToString(in.bytes(compactAliasBytes))+"."+domain, port)
		}
		payload.Candidates = append(payload.Candidates, candidate)
	}
	if in.err != nil || len(in.data) != 0 {
		return PairingPayload{}, errors.New("compact pairing has the wrong length")
	}
	for _, candidate := range payload.Candidates {
		if candidate.AdmissionURL != "" {
			return payload, nil
		}
	}
	return PairingPayload{}, errors.New("compact pairing has no admission")
}

// connectorURLHost reads a URL built by candidateURL.
func connectorURLHost(raw string) (string, int, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "wss" || parsed.Path != "/ws" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", 0, fmt.Errorf("compact pairing cannot carry %q", raw)
	}
	host, port := parsed.Host, 443
	if parsed.Port() != "" {
		value, err := strconv.Atoi(parsed.Port())
		if err != nil || value <= 0 || value > 65535 {
			return "", 0, fmt.Errorf("compact pairing cannot carry %q", raw)
		}
		host, port = parsed.Hostname(), value
	}
	if host == "" || strings.Contains(host, ":") || net.ParseIP(host) != nil {
		return "", 0, fmt.Errorf("compact pairing cannot carry %q", raw)
	}
	return host, port, nil
}

func fixedHex(value string, size int) ([]byte, error) {
	raw, err := hex.DecodeString(value)
	if err != nil || len(raw) != size || hex.EncodeToString(raw) != value {
		return nil, fmt.Errorf("compact pairing needs %d bytes of lowercase hex", size)
	}
	return raw, nil
}

func writeShortString(out *bytes.Buffer, value string) error {
	if len(value) > 255 {
		return errors.New("compact pairing text longer than 255 bytes")
	}
	out.WriteByte(byte(len(value)))
	out.WriteString(value)
	return nil
}

type compactReader struct {
	data []byte
	err  error
}

func (r *compactReader) bytes(n int) []byte {
	if r.err != nil || len(r.data) < n {
		r.err = errors.New("short")
		return make([]byte, n)
	}
	out := r.data[:n]
	r.data = r.data[n:]
	return out
}

func (r *compactReader) byte() byte { return r.bytes(1)[0] }

func (r *compactReader) shortString() string {
	return string(r.bytes(int(r.byte())))
}
