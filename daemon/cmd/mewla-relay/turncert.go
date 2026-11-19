package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/account/dns"
	"github.com/daoleno/mewla/daemon/acmecert"
	"github.com/daoleno/mewla/daemon/relay"
)

// turnCloudflareTokenEnv holds the Cloudflare token (DNS edit on the zone)
// TURN's certificate renews with.
const turnCloudflareTokenEnv = "MEWLA_TURN_CLOUDFLARE_TOKEN"

// turnCertFlags keep TURN's TLS certificate valid by itself (package
// acmecert): issued and renewed by DNS-01, served without a restart.
type turnCertFlags struct {
	dir, domain, directory, caFile       string
	dnsProvider, zoneID, challtestsrvURL string
	renewBefore, propagation, checkEvery time.Duration
}

func (f *turnCertFlags) register(flags *flag.FlagSet) {
	flags.StringVar(&f.dir, "turn-tls-acme-dir", "", "keep TURN's TLS certificate valid by itself: a writable directory for the ACME account key and the issued certificate (renewed by DNS-01; -turn-tls-cert/-key are served until it holds a later one; SIGHUP renews at once)")
	flags.StringVar(&f.domain, "turn-tls-acme-domain", "", "the name on TURN's certificate, such as turn.connect.example")
	flags.StringVar(&f.directory, "turn-tls-acme-directory", acmecert.LetsEncrypt, "ACME directory URL")
	flags.StringVar(&f.caFile, "turn-tls-acme-ca-file", "", "PEM roots for the ACME directory (a test CA; default: the system's)")
	flags.StringVar(&f.dnsProvider, "turn-tls-acme-dns-provider", "cloudflare", "DNS-01 provider: cloudflare (token in $"+turnCloudflareTokenEnv+") or challtestsrv")
	flags.StringVar(&f.zoneID, "turn-tls-acme-cloudflare-zone-id", "", "Cloudflare zone id of the name")
	flags.StringVar(&f.challtestsrvURL, "turn-tls-acme-challtestsrv-url", "http://127.0.0.1:8055", "pebble-challtestsrv management API URL")
	flags.DurationVar(&f.renewBefore, "turn-tls-acme-renew-before", 30*24*time.Hour, "renew this long before the certificate expires")
	flags.DurationVar(&f.propagation, "turn-tls-acme-propagation", 30*time.Second, "wait after publishing the challenge before the CA checks it")
	flags.DurationVar(&f.checkEvery, "turn-tls-acme-check-every", 12*time.Hour, "longest wait between looks at the expiry")
}

// keeper is nil when -turn-tls-acme-dir is not set.
func (f *turnCertFlags) keeper(seedCert, seedKey string, logf func(string, ...any)) (*acmecert.Keeper, error) {
	if strings.TrimSpace(f.dir) == "" {
		return nil, nil
	}
	if strings.TrimSpace(f.domain) == "" {
		return nil, errors.New("-turn-tls-acme-dir needs -turn-tls-acme-domain")
	}
	var provider acmecert.DNS
	switch f.dnsProvider {
	case "cloudflare":
		token := strings.TrimSpace(os.Getenv(turnCloudflareTokenEnv))
		if token == "" || strings.TrimSpace(f.zoneID) == "" {
			return nil, fmt.Errorf("-turn-tls-acme-dns-provider cloudflare needs -turn-tls-acme-cloudflare-zone-id and $%s", turnCloudflareTokenEnv)
		}
		provider = &dns.Cloudflare{ZoneID: strings.TrimSpace(f.zoneID), Token: token}
	case "challtestsrv":
		provider = &dns.Challtestsrv{BaseURL: f.challtestsrvURL}
	default:
		return nil, errors.New("-turn-tls-acme-dns-provider must be cloudflare or challtestsrv")
	}
	client := http.DefaultClient
	if f.caFile != "" {
		pemBytes, err := os.ReadFile(f.caFile)
		if err != nil {
			return nil, fmt.Errorf("-turn-tls-acme-ca-file: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pemBytes) {
			return nil, errors.New("-turn-tls-acme-ca-file holds no certificate")
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
		client = &http.Client{Transport: transport}
	}
	return acmecert.New(acmecert.Config{
		Domain: f.domain, Dir: f.dir, Directory: f.directory, HTTPClient: client, DNS: provider,
		RenewBefore: f.renewBefore, Propagation: f.propagation, CheckEvery: f.checkEvery,
		SeedCert: seedCert, SeedKey: seedKey, Logf: logf,
	})
}

// turnOperatorHandler is a TURN-only process's health and metrics: whether
// it serves, its allocations, and its TLS certificate's days until expiry.
func turnOperatorHandler(srv *relay.TURNServer, keeper *acmecert.Keeper) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		body := map[string]any{"status": "ok"}
		code := http.StatusOK
		if keeper != nil {
			st := keeper.Status()
			cert := map[string]any{
				"domain": st.Domain, "days_left": roundDays(st.DaysLeft(time.Now())),
				"renewal_failures": st.Failures,
			}
			if !st.NotAfter.IsZero() {
				cert["not_after"] = st.NotAfter.UTC().Format(time.RFC3339)
			}
			if !st.Renewed.IsZero() {
				cert["renewed"] = st.Renewed.UTC().Format(time.RFC3339)
			}
			if st.LastError != "" {
				cert["last_error"] = st.LastError
			}
			body["tls_certificate"] = cert
			if st.NotAfter.IsZero() || !time.Now().Before(st.NotAfter) {
				body["status"] = "tls certificate missing or expired"
				code = http.StatusServiceUnavailable
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		total, _ := srv.Allocations("")
		fmt.Fprintf(w, "# TYPE mewla_turn_allocations gauge\nmewla_turn_allocations %d\n", total)
		if keeper != nil {
			st := keeper.Status()
			fmt.Fprintf(w, "# TYPE mewla_turn_tls_certificate_days_left gauge\nmewla_turn_tls_certificate_days_left %.2f\n", st.DaysLeft(time.Now()))
			fmt.Fprintf(w, "# TYPE mewla_turn_tls_certificate_not_after_seconds gauge\nmewla_turn_tls_certificate_not_after_seconds %d\n", unixOrZero(st.NotAfter))
			fmt.Fprintf(w, "# TYPE mewla_turn_tls_renewal_failures gauge\nmewla_turn_tls_renewal_failures %d\n", st.Failures)
		}
	})
	return mux
}

func roundDays(d float64) float64 { return float64(int(d*10)) / 10 }

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}
