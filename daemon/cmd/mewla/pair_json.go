package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/control"
	"rsc.io/qr/coding"
)

// pairingJSON is `mewla pair -json`: what a desktop app around this daemon
// needs to show the phone QR and to pair itself, without reading the
// terminal's words.
type pairingJSON struct {
	ExpiresAt       time.Time `json:"expires_at"`
	DaemonID        string    `json:"daemon_id"`
	DaemonPublicKey string    `json:"daemon_public_key"`
	// PhoneAddress is where the phone link points (an address, or Mewla
	// Relay); empty with PhoneLink when no phone can reach this computer.
	PhoneAddress string `json:"phone_address,omitempty"`
	PhoneLink    string `json:"phone_link,omitempty"`
	// PhoneQR is PhoneLink's QR code: Size modules a side, one string of
	// '1' (dark) and '0' per row, without the quiet zone. Encoded at level Q,
	// so the app may cover its centre with the mark (up to a tenth of it).
	PhoneQR *pairingQR `json:"phone_qr,omitempty"`
	// WifiAddress and WifiLink pair a phone on this Wi-Fi (the best LAN
	// address), WifiQR its code; the same as the phone's when that is LAN.
	WifiAddress string     `json:"wifi_address,omitempty"`
	WifiLink    string     `json:"wifi_link,omitempty"`
	WifiQR      *pairingQR `json:"wifi_qr,omitempty"`
	// LocalLink is the connect link of this computer's own address, for an
	// app on this computer.
	LocalLink    string `json:"local_link,omitempty"`
	LocalBrowser string `json:"local_browser,omitempty"`
}

type pairingQR struct {
	Size int      `json:"size"`
	Rows []string `json:"rows"`
}

func writePairingJSON(w io.Writer, info *control.PairingInfo, links pairingLinks) error {
	out := pairingJSON{
		ExpiresAt:       info.ExpiresAt,
		DaemonID:        info.DaemonID,
		DaemonPublicKey: info.DaemonPublicKey,
		PhoneAddress:    links.phoneAddress,
		PhoneLink:       links.phoneLink,
		LocalLink:       links.localLink,
		LocalBrowser:    links.localBrowser,
		WifiAddress:     links.wifiAddress,
		WifiLink:        links.wifiLink,
	}
	var err error
	if out.PhoneQR, err = pairingQRModules(links.phoneLink); err != nil {
		return fmt.Errorf("encode the phone QR: %w", err)
	}
	if out.WifiQR, err = pairingQRModules(links.wifiLink); err != nil {
		return fmt.Errorf("encode the Wi-Fi QR: %w", err)
	}
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(out)
}

// pairingQRModules is link's QR code at level Q (nil for no link).
func pairingQRModules(link string) (*pairingQR, error) {
	if link == "" {
		return nil, nil
	}
	code, err := encodeQRAt(link, coding.Q)
	if err != nil {
		return nil, err
	}
	qr := &pairingQR{Size: code.Size, Rows: make([]string, code.Size)}
	for y := 0; y < code.Size; y++ {
		var row strings.Builder
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				row.WriteByte('1')
			} else {
				row.WriteByte('0')
			}
		}
		qr.Rows[y] = row.String()
	}
	return qr, nil
}
