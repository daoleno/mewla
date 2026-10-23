package main

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/addressbook"
	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/pairlink"
	"github.com/daoleno/mewla/daemon/remote"
	"github.com/daoleno/mewla/daemon/server"
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
	// Loopback first: https://127.0.0.1:<port> is still unreachable from a
	// phone, and must never outrank a real address in the phone QR.
	if isLoopbackHost(parsed.Hostname()) {
		return kindThisComputer
	}
	if parsed.Scheme == "https" {
		return kindHTTPS
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
	localShort   string // http://127.0.0.1:9876/p/<token>, when the daemon serves it
	httpsBrowser []string
}

// shortBrowserLink is this computer's one-line browser pairing link; the
// daemon redirects it to the full pairing page.
func shortBrowserLink(origin, tokenHex string) string {
	token, err := hex.DecodeString(strings.TrimSpace(tokenHex))
	if err != nil {
		return ""
	}
	return strings.TrimRight(origin, "/") + "/p/" + base64.RawURLEncoding.EncodeToString(token)
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
			links.localShort = shortBrowserLink(entry.URL, pairing.Value)
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

// pairingStatus is the one-glance state shown above a pairing code.
type pairingStatus struct {
	remoteURL string // "" when remote access is off
	devices   int
}

func pairingStatusFor(stateDir string, devices int) pairingStatus {
	status := pairingStatus{devices: devices}
	if config, ok, err := remote.LoadConfig(stateDir); err == nil && ok {
		status.remoteURL = config.URL()
	}
	return status
}

func printStatus(w io.Writer, style termStyle, status pairingStatus) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, style.title("Mewla is running"))
	if status.remoteURL != "" {
		fmt.Fprintln(w, style.check(true, "Remote access", status.remoteURL))
	} else {
		fmt.Fprintln(w, style.check(false, "Remote access", "off "+style.dim("· turn on: ")+style.cmd("mewla login")))
	}
	if status.devices == 0 {
		fmt.Fprintln(w, style.check(false, "Paired devices", "none yet"))
	} else {
		fmt.Fprintln(w, style.check(true, "Paired devices", fmt.Sprint(status.devices)))
	}
}

// phoneReachLine says what the phone needs to reach the address in the QR.
func phoneReachLine(address string) string {
	if strings.HasPrefix(address, "Remote access") || address == "Mewla Link" {
		return "Works from any network."
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" {
		return "Phone connects to " + address + "."
	}
	switch classifyAddress(address) {
	case kindTailscale:
		return "Over Tailscale (" + parsed.Hostname() + "): turn Tailscale on in your phone too."
	case kindLAN:
		return "Over Wi-Fi (" + parsed.Hostname() + "): your phone must be on the same network."
	}
	return "Phone connects to " + parsed.Host + "."
}

// printPairing is the one-screen pairing view: status, the phone QR with
// where to scan it, and a short link for this computer's browser. The paste
// link, other addresses and revoking live in printPairingDetails.
func printPairing(w io.Writer, status pairingStatus, links pairingLinks, stateDir string) {
	style := styleForWriter(w)
	detailsCommand := pairCommand(stateDir) + " --details"
	printStatus(w, style, status)
	fmt.Fprintln(w)
	if links.phoneLink != "" {
		fmt.Fprintln(w, style.bold("Pair your phone"))
		need := qrColumns(links.phoneLink)
		if width := terminalColumns(w); width > 0 && need > width {
			fmt.Fprintf(w, "  The QR code needs a window %d columns wide. Widen this one and run %s,\n", need, style.cmd("mewla pair"))
			fmt.Fprintf(w, "  or paste the link from %s into the app.\n", style.cmd(detailsCommand))
		} else {
			fmt.Fprintln(w)
			if err := renderQR(w, style, links.phoneLink); err != nil {
				fmt.Fprintf(w, "  Could not draw the QR code: %v\n", err)
			}
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, "  In the Mewla app, tap “Mewla already running? Scan its code”,")
		fmt.Fprintln(w, "  or go to Settings → Pair a computer.")
		fmt.Fprintln(w, "  "+phoneReachLine(links.phoneAddress))
		fmt.Fprintln(w, "  "+style.dim("The code works once, until "+links.expiresAt.Local().Format("15:04")+"."))
	} else {
		fmt.Fprintln(w, style.bold("Phones can't reach this computer yet"))
		fmt.Fprintf(w, "  From anywhere: %s    Same Wi-Fi: %s\n", style.cmd("mewla login"), style.cmd("mewla --lan"))
	}
	browser := links.localShort
	if browser == "" {
		browser = links.localBrowser
	}
	if browser != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, style.bold("Browser on this computer"))
		fmt.Fprintln(w, "  "+browser)
		fmt.Fprintln(w, "  "+style.dim("Or let Mewla open and pair it: ")+style.cmd(webCommand(stateDir)))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, style.dim("Paste link, other addresses, revoking a device: ")+style.cmd(detailsCommand))
	fmt.Fprintln(w)
}

// printRunningStatus is what plain mewla shows when Mewla already runs and
// has paired devices.
func printRunningStatus(w io.Writer, stateDir string, devices int) {
	style := styleForWriter(w)
	resolved, err := auth.ResolveStorageDir(stateDir)
	if err != nil {
		resolved = stateDir
	}
	printStatus(w, style, pairingStatusFor(resolved, devices))
	fmt.Fprintln(w)
	fmt.Fprintln(w, style.dim("Open the web UI here: ")+style.cmd(webCommand(stateDir)))
	fmt.Fprintln(w, style.dim("Pair another phone or browser: ")+style.cmd(pairCommand(stateDir)))
	fmt.Fprintln(w)
}

func stateDirArgs(stateDir string) []string {
	if strings.TrimSpace(stateDir) == "" {
		return nil
	}
	return []string{"-state-dir", stateDir}
}

// printPairingDetails is everything one pairing code can be used through.
func printPairingDetails(w io.Writer, title string, links pairingLinks) {
	style := styleForWriter(w)
	fmt.Fprintf(w, "\n%s This code works once and expires at %s.\n", style.bold(title), links.expiresAt.Local().Format("15:04"))
	fmt.Fprintln(w, "Pairing gives that device access to sessions, terminal, Brain, Workers and files.")
	fmt.Fprintln(w)
	if links.phoneLink != "" {
		fmt.Fprintln(w, "Phone — in Mewla, Settings → Pair a computer, scan:")
		if width := terminalColumns(w); width == 0 || qrColumns(links.phoneLink) <= width {
			if err := renderQR(w, style, links.phoneLink); err != nil {
				fmt.Fprintf(w, "Could not draw the QR code: %v\n", err)
			}
		}
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

// printFirstDevicePairing shows the pairing screen when no device is trusted
// yet, and reports whether it did. A pairing code is a credential, so output
// that is not a person's terminal (a launchd or systemd log file) only gets
// the command that shows one.
func printFirstDevicePairing(w io.Writer, manager *auth.Manager, book *addressbook.Store, stateDir string, interactive bool) (bool, error) {
	if len(manager.ListDevices()) != 0 {
		return false, nil
	}
	if !interactive {
		fmt.Fprintf(w, "No phone or browser is paired yet. Run %s to show a pairing code.\n", pairCommand(stateDir))
		return false, nil
	}
	entries, err := book.List()
	if err != nil {
		return false, err
	}
	if len(entries) == 0 {
		return false, nil
	}
	token, err := manager.IssuePairingToken(auth.DefaultPairingTTL)
	if err != nil {
		return false, err
	}
	links, err := buildPairingLinks(entries, "", manager.PublicKeyHex(), token)
	if err != nil {
		return false, err
	}
	printPairing(w, pairingStatusFor(manager.StorageDir(), 0), links, stateDir)
	return true, nil
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

// directEndpoints are the addresses a phone on the same network or tailnet can
// dial, for the address this daemon listens on. A loopback daemon has none: it
// is reachable only through its relay.
func directEndpoints(listenAddr string, detected []privateNetworkAddress) []server.DirectEndpoint {
	host, portText, err := net.SplitHostPort(listenAddr)
	if err != nil || isLoopbackHost(host) {
		return nil
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return nil
	}
	var endpoints []server.DirectEndpoint
	for _, address := range startupPairingAddresses(host, detected) {
		kind := "lan"
		if isTailscaleAddress(address.label, address.ip) {
			kind = "tailscale"
		}
		endpoints = append(endpoints, server.DirectEndpoint{Host: address.ip.String(), Port: port, Kind: kind})
	}
	return endpoints
}

// printStartupInfo is what a foreground start shows. After a pairing screen
// it is one dim line; otherwise it is the status with this computer's
// addresses.
func printStartupInfo(w io.Writer, listenAddr, stateDir, dataDir string, linkEnabled bool, entries []addressbook.Entry, afterPairing bool) {
	style := styleForWriter(w)
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
	if afterPairing {
		fmt.Fprintln(w, style.dim(fmt.Sprintf("Mewla %s · %s · Ctrl-C stops it", Version, listening)))
		if strings.TrimSpace(dataDir) != "" {
			fmt.Fprintln(w, style.dim("Data in "+dataDir))
		}
		fmt.Fprintln(w)
		return
	}
	fmt.Fprintf(w, "\n%s %s\n", style.title("Mewla "+Version), style.dim("· listening on "+listening))
	if strings.TrimSpace(dataDir) != "" {
		fmt.Fprintf(w, "  %-11s %s\n", "Data", dataDir)
	}
	if linkEnabled {
		fmt.Fprintf(w, "  %-11s %s\n", "Mewla Link", "connecting outbound")
	}
	phone := rankedPhoneAddresses(entries)
	sort.SliceStable(phone, func(i, j int) bool {
		return classifyAddress(phone[i].URL).phoneRank() > classifyAddress(phone[j].URL).phoneRank()
	})
	for _, entry := range phone {
		fmt.Fprintf(w, "  %-11s %s\n", entryLabel(entry), entry.URL)
	}
	if hint := remoteHint(dataDir); hint != "" && !linkEnabled {
		fmt.Fprintf(w, "  %-11s %s\n", "Anywhere", style.cmd("mewla login"))
	}
	if err == nil && (isLoopbackHost(host) || isWildcardHost(host)) {
		fmt.Fprintf(w, "  %-11s %s\n", "Browser", style.cmd(webCommand(stateDir)))
	}
	if linkEnabled || len(phone) > 0 {
		fmt.Fprintf(w, "  %-11s %s\n\n", "Pair", style.cmd(pairCommand(stateDir)))
		return
	}
	if err == nil && isWildcardHost(host) {
		fmt.Fprintln(w, "  No Wi-Fi/LAN or Tailscale address found.")
	}
	fmt.Fprintf(w, "  %-11s restart with %s, or add an HTTPS address (%s)\n\n", "Phone", style.cmd("mewla --lan"), style.cmd("mewla address add"))
}

// entryLabel names an address for people; the hosted address is "Remote".
func entryLabel(entry addressbook.Entry) string {
	if entry.Source == addressbook.SourceRemote {
		return "Remote"
	}
	return classifyAddress(entry.URL).label()
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
	return pairlink.NormalizeEndpoint(rawValue)
}

func buildConnectLinkWithPublicKey(
	serverURL string,
	daemonPublicKey string,
	pairing auth.PairingToken,
) string {
	link, err := pairlink.ConnectLink(serverURL, daemonPublicKey, pairing.Value)
	if err != nil {
		return "mewla://settings"
	}
	return link
}
