package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/addressbook"
	"github.com/daoleno/mewla/daemon/auth"
	"github.com/mdp/qrterminal/v3"
)

const (
	connectParamPayload   = "p"
	connectPayloadVersion = 1
	connectPublicKeyBytes = 32
	connectTokenBytes     = 32
)

type connectionOffer struct {
	URL         string
	ConnectLink string
}

type privateNetworkAddress struct {
	label string
	ip    net.IP
}

func buildConnectionOffersWithPublicKey(
	endpoint string,
	daemonPublicKey string,
	pairing auth.PairingToken,
) ([]connectionOffer, error) {
	if strings.TrimSpace(endpoint) == "" {
		return nil, nil
	}

	normalizedURL, err := normalizeEndpoint(endpoint)
	if err != nil {
		return nil, err
	}

	offer := connectionOffer{URL: normalizedURL}
	offer.ConnectLink = buildConnectLinkWithPublicKey(
		offer.URL,
		daemonPublicKey,
		pairing,
	)
	return []connectionOffer{offer}, nil
}

// addressKind is the reader-facing class of an address: how a phone or
// browser gets to it.
type addressKind string

const (
	kindThisComputer addressKind = "this-computer"
	kindLAN          addressKind = "lan"
	kindTailscale    addressKind = "tailscale"
	kindHTTPS        addressKind = "https"
)

func (k addressKind) label() string {
	switch k {
	case kindThisComputer:
		return "This computer"
	case kindLAN:
		return "Wi-Fi/LAN"
	case kindTailscale:
		return "Tailscale"
	case kindHTTPS:
		return "HTTPS"
	}
	return string(k)
}

// phoneRank orders addresses for a phone QR; loopback is unreachable from a
// phone and ranks nowhere.
func (k addressKind) phoneRank() int {
	switch k {
	case kindHTTPS:
		return 0
	case kindTailscale:
		return 1
	case kindLAN:
		return 2
	}
	return -1
}

func classifyAddress(raw string) addressKind {
	parsed, err := url.Parse(raw)
	if err != nil {
		return kindLAN
	}
	if parsed.Scheme == "https" {
		return kindHTTPS
	}
	if isLoopbackHost(parsed.Hostname()) {
		return kindThisComputer
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && isTailscaleAddress("", ip) {
		return kindTailscale
	}
	return kindLAN
}

// rankedPhoneAddresses returns the entries a phone can reach, best first:
// HTTPS, then Tailscale, then Wi-Fi/LAN, each most recently seen first.
func rankedPhoneAddresses(entries []addressbook.Entry) []addressbook.Entry {
	ranked := make([]addressbook.Entry, 0, len(entries))
	for _, entry := range entries {
		if classifyAddress(entry.URL).phoneRank() >= 0 {
			ranked = append(ranked, entry)
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		left, right := classifyAddress(ranked[i].URL).phoneRank(), classifyAddress(ranked[j].URL).phoneRank()
		if left != right {
			return left < right
		}
		return ranked[i].LastSeenAt.After(ranked[j].LastSeenAt)
	})
	return ranked
}

// pairingLinks is everything one pairing code can be used through. The code
// works once, for whichever phone or browser uses it first.
type pairingLinks struct {
	expiresAt    time.Time
	phoneAddress string // "Mewla Link" or the URL in the QR; empty when no phone can reach this computer
	phoneLink    string
	localBrowser string
	httpsBrowser []string
}

// buildPairingLinks puts phoneAddress (or the best phone address in the book)
// in the phone QR, and adds a browser link for this computer and for every
// HTTPS address, where the daemon serves the web UI.
func buildPairingLinks(
	entries []addressbook.Entry,
	phoneAddress string,
	daemonPublicKey string,
	pairing auth.PairingToken,
) (pairingLinks, error) {
	links := pairingLinks{expiresAt: pairing.ExpiresAt}
	if phoneAddress == "" {
		if ranked := rankedPhoneAddresses(entries); len(ranked) > 0 {
			phoneAddress = ranked[0].URL
		}
	}
	if phoneAddress != "" {
		offers, err := buildConnectionOffersWithPublicKey(phoneAddress, daemonPublicKey, pairing)
		if err != nil {
			return links, err
		}
		links.phoneAddress = strings.TrimRight(phoneAddress, "/")
		links.phoneLink = offers[0].ConnectLink
	}
	var localSeen time.Time
	for _, entry := range entries {
		switch classifyAddress(entry.URL) {
		case kindThisComputer:
			if links.localBrowser != "" && !entry.LastSeenAt.After(localSeen) {
				continue
			}
			browser, err := buildWebPairingURL(entry.URL, daemonPublicKey, pairing)
			if err != nil {
				return links, err
			}
			links.localBrowser, localSeen = browser, entry.LastSeenAt
		case kindHTTPS:
			browser, err := buildWebPairingURL(entry.URL, daemonPublicKey, pairing)
			if err != nil {
				return links, err
			}
			links.httpsBrowser = append(links.httpsBrowser, browser)
		}
	}
	return links, nil
}

// buildWebPairingURL carries the connect link in the URL fragment, which
// browsers never send to the daemon or a proxy.
func buildWebPairingURL(origin, daemonPublicKey string, pairing auth.PairingToken) (string, error) {
	origin = strings.TrimRight(origin, "/")
	offers, err := buildConnectionOffersWithPublicKey(origin, daemonPublicKey, pairing)
	if err != nil {
		return "", fmt.Errorf("build connection info: %w", err)
	}
	fragment := url.Values{}
	fragment.Set("pair", offers[0].ConnectLink)
	return origin + "/#" + fragment.Encode(), nil
}

func printPairing(w io.Writer, title string, links pairingLinks) {
	fmt.Fprintf(w, "\n%s This code works once and expires at %s.\n", title, links.expiresAt.Local().Format("15:04"))
	fmt.Fprintln(w, "Pairing gives that device access to sessions, terminal, Brain, Workers and files.")
	fmt.Fprintln(w)
	if links.phoneLink != "" {
		fmt.Fprintln(w, "Phone — in Mewla, Settings → Pair a computer, scan:")
		renderPairingQR(w, links.phoneLink)
		fmt.Fprintf(w, "or paste:  %s\n", links.phoneLink)
		fmt.Fprintf(w, "Address:   %s   (other addresses: mewla address list)\n", links.phoneAddress)
	} else {
		fmt.Fprintln(w, "Phones can't reach this computer yet. Restart with mewla --lan for Wi-Fi/LAN")
		fmt.Fprintln(w, "and Tailscale, or put HTTPS in front of it and run mewla address add https://…")
	}
	if links.localBrowser != "" || len(links.httpsBrowser) > 0 {
		fmt.Fprintln(w)
	}
	if links.localBrowser != "" {
		fmt.Fprintf(w, "Browser on this computer:  %s\n", links.localBrowser)
	}
	for _, browser := range links.httpsBrowser {
		fmt.Fprintf(w, "Browser anywhere:          %s\n", browser)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Revoke a device later: mewla devices revoke -id DEVICE_ID")
	fmt.Fprintln(w)
}

// Print bootstrap credentials only when there is no trusted device yet.
func printFirstDevicePairing(w io.Writer, manager *auth.Manager, book *addressbook.Store) error {
	if len(manager.ListDevices()) != 0 {
		return nil
	}
	entries, err := book.List()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	token, err := manager.IssuePairingToken(auth.DefaultPairingTTL)
	if err != nil {
		return err
	}
	links, err := buildPairingLinks(entries, "", manager.PublicKeyHex(), token)
	if err != nil {
		return err
	}
	printPairing(w, "First device: pair a phone or browser.", links)
	return nil
}

// startupAddresses is what the daemon serves after binding listenAddr: this
// computer when it listens on loopback or everywhere, and the detected
// Wi-Fi/LAN and Tailscale addresses it actually listens on.
func startupAddresses(listenAddr string, detected []privateNetworkAddress) []string {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return nil
	}
	var addresses []string
	if isLoopbackHost(host) || isWildcardHost(host) {
		addresses = append(addresses, "http://"+net.JoinHostPort("127.0.0.1", port))
	}
	if !isLoopbackHost(host) {
		for _, address := range startupPairingAddresses(host, detected) {
			addresses = append(addresses, "http://"+net.JoinHostPort(address.ip.String(), port))
		}
	}
	return addresses
}

func printStartupInfo(w io.Writer, listenAddr, stateDir string, linkEnabled bool, entries []addressbook.Entry) {
	host, _, err := net.SplitHostPort(listenAddr)
	listening := listenAddr
	switch {
	case err != nil:
	case isLoopbackHost(host):
		listening += " (this computer only)"
	case isWildcardHost(host):
		_, port, _ := net.SplitHostPort(listenAddr)
		listening = "all networks, port " + port
	}
	fmt.Fprintf(w, "\n  Mewla %s · listening on %s\n", Version, listening)
	if linkEnabled {
		fmt.Fprintf(w, "  %-11s %s\n", "Mewla Link", "connecting outbound")
	}
	phone := rankedPhoneAddresses(entries)
	sort.SliceStable(phone, func(i, j int) bool {
		return classifyAddress(phone[i].URL).phoneRank() > classifyAddress(phone[j].URL).phoneRank()
	})
	for _, entry := range phone {
		fmt.Fprintf(w, "  %-11s %s\n", classifyAddress(entry.URL).label(), entry.URL)
	}
	if linkEnabled || len(phone) > 0 {
		fmt.Fprintf(w, "  %-11s %s\n\n", "Pair", pairCommand(stateDir))
		return
	}
	if err == nil && isWildcardHost(host) {
		fmt.Fprintln(w, "  No Wi-Fi/LAN or Tailscale address found.")
	}
	fmt.Fprintf(w, "  %-11s %s, then open the browser link\n", "Browser", pairCommand(stateDir))
	fmt.Fprintf(w, "  %-11s restart with mewla --lan, or add an HTTPS address (mewla address add)\n\n", "Phone")
}

func pairCommand(stateDir string) string {
	if strings.TrimSpace(stateDir) != "" {
		return "mewla pair -state-dir " + stateDir
	}
	return "mewla pair"
}

func detectPrivateNetworkAddresses() []privateNetworkAddress {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var detected []privateNetworkAddress
	seen := make(map[string]bool)
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if shouldSkipPrivateNetworkInterface(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil || ip.To4() == nil {
				continue
			}
			ip = ip.To4()
			label := ""
			switch {
			case isTailscaleAddress(iface.Name, ip):
				label = "Tailscale"
			case ip.IsPrivate():
				label = "Same Wi-Fi/LAN"
			}
			key := ip.String()
			if label != "" && !seen[key] {
				seen[key] = true
				detected = append(detected, privateNetworkAddress{label: label, ip: ip})
			}
		}
	}
	sort.Slice(detected, func(i, j int) bool {
		if detected[i].label != detected[j].label {
			return detected[i].label < detected[j].label
		}
		return bytesCompare(detected[i].ip, detected[j].ip) < 0
	})
	return detected
}

func shouldSkipPrivateNetworkInterface(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range []string{"docker", "br-", "veth", "virbr", "cni", "podman", "kube"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func startupPairingAddresses(host string, detected []privateNetworkAddress) []privateNetworkAddress {
	if isWildcardHost(host) {
		return detected
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || ip.IsLoopback() {
		return nil
	}
	for _, address := range detected {
		if address.ip.Equal(ip) {
			return []privateNetworkAddress{address}
		}
	}
	if ip.To4() != nil && ip.IsPrivate() {
		return []privateNetworkAddress{{label: "Private network", ip: ip.To4()}}
	}
	return nil
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

func isWildcardHost(host string) bool {
	host = strings.Trim(host, "[]")
	return host == "" || host == "0.0.0.0" || host == "::"
}

func isTailscaleAddress(interfaceName string, ip net.IP) bool {
	if strings.HasPrefix(strings.ToLower(interfaceName), "tailscale") {
		return true
	}
	ip4 := ip.To4()
	return ip4 != nil && ip4[0] == 100 && ip4[1]&0xc0 == 0x40
}

func bytesCompare(left, right net.IP) int {
	for index := 0; index < len(left) && index < len(right); index++ {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return len(left) - len(right)
}

func normalizeEndpoint(rawValue string) (string, error) {
	trimmed := strings.TrimSpace(rawValue)
	if trimmed == "" {
		return "", fmt.Errorf("endpoint is empty")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("endpoint must include scheme and host")
	}

	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported endpoint scheme %q", parsed.Scheme)
	}

	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/ws"
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

func buildConnectLinkWithPublicKey(
	serverURL string,
	daemonPublicKey string,
	pairing auth.PairingToken,
) string {
	payload, err := encodeConnectPayload(
		serverURL,
		daemonPublicKey,
		pairing.Value,
	)
	if err != nil {
		return "mewla://settings"
	}
	params := url.Values{}
	params.Set(connectParamPayload, payload)
	return "mewla://settings?" + params.Encode()
}

func renderPairingQR(w io.Writer, link string) {
	qrterminal.GenerateWithConfig(link, qrterminal.Config{
		Level:          qrterminal.L,
		Writer:         w,
		HalfBlocks:     true,
		BlackChar:      qrterminal.BLACK_BLACK,
		WhiteBlackChar: qrterminal.WHITE_BLACK,
		WhiteChar:      qrterminal.WHITE_WHITE,
		BlackWhiteChar: qrterminal.BLACK_WHITE,
		QuietZone:      1,
	})
}

func encodeConnectPayload(serverURL, daemonPublicKeyHex, enrollmentTokenHex string) (string, error) {
	urlBytes := []byte(strings.TrimSpace(serverURL))
	if len(urlBytes) == 0 {
		return "", fmt.Errorf("server URL is empty")
	}
	if len(urlBytes) > 0xffff {
		return "", fmt.Errorf("server URL is too long")
	}

	publicKey, err := hex.DecodeString(strings.TrimSpace(daemonPublicKeyHex))
	if err != nil {
		return "", fmt.Errorf("decode daemon public key: %w", err)
	}
	if len(publicKey) != connectPublicKeyBytes {
		return "", fmt.Errorf("daemon public key must be %d bytes", connectPublicKeyBytes)
	}

	token, err := hex.DecodeString(strings.TrimSpace(enrollmentTokenHex))
	if err != nil {
		return "", fmt.Errorf("decode enrollment token: %w", err)
	}
	if len(token) != connectTokenBytes {
		return "", fmt.Errorf("enrollment token must be %d bytes", connectTokenBytes)
	}

	payload := make([]byte, 1+2+len(urlBytes)+len(publicKey)+len(token))
	payload[0] = connectPayloadVersion
	binary.BigEndian.PutUint16(payload[1:3], uint16(len(urlBytes)))

	offset := 3
	copy(payload[offset:], urlBytes)
	offset += len(urlBytes)
	copy(payload[offset:], publicKey)
	offset += len(publicKey)
	copy(payload[offset:], token)

	return base64.RawURLEncoding.EncodeToString(payload), nil
}
