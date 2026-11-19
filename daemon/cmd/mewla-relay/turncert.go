package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/account/dns"
	"github.com/daoleno/mewla/daemon/acmecert"
	"github.com/daoleno/mewla/daemon/relay"
)

// turnCloudflareTokenEnv holds the Cloudflare token (DNS edit on the zone)
// the renewer of TURN's certificate writes its challenge with.
const turnCloudflareTokenEnv = "MEWLA_TURN_CLOUDFLARE_TOKEN"

// turnCertFlags split TURN's certificate between two processes: the renewer
// (-turn-cert-renew: holds the DNS credential, listens on nothing, writes the
// certificate into a directory) and the TURN server (-turn-tls-cert-dir:
// reads that directory, never holds the credential).
type turnCertFlags struct {
	renewOnly                            bool
	certDir                              string
	dir, accountKey, domain              string
	directory, caFile                    string
	dnsProvider, zoneID, challtestsrvURL string
	renewBefore, propagation, checkEvery time.Duration
}

func (f *turnCertFlags) register(flags *flag.FlagSet) {
	flags.StringVar(&f.certDir, "turn-tls-cert-dir", "", "serve TURN's TLS certificate from this directory (cert.pem, key.pem and status.json, as -turn-cert-renew writes them), read only and reloaded when it changes; -turn-tls-cert/-key are served until it holds a later one; SIGHUP reads it at once")
	flags.BoolVar(&f.renewOnly, "turn-cert-renew", false, "run only the renewer of TURN's certificate: no listener, no TURN; it keeps -turn-tls-acme-dir valid by DNS-01 (SIGHUP renews at once)")
	flags.StringVar(&f.dir, "turn-tls-acme-dir", "", "-turn-cert-renew: the directory the certificate, its key and status.json are written to (the TURN server's -turn-tls-cert-dir)")
	flags.StringVar(&f.accountKey, "turn-tls-acme-account-key", "", "-turn-cert-renew: the ACME account key's file, outside the certificate directory (default: <dir>/account.pem)")
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

func (f *turnCertFlags) keeper(seedCert, seedKey string, logf func(string, ...any)) (*acmecert.Keeper, error) {
	if strings.TrimSpace(f.dir) == "" || strings.TrimSpace(f.domain) == "" {
		return nil, errors.New("-turn-cert-renew needs -turn-tls-acme-dir and -turn-tls-acme-domain")
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
		Domain: f.domain, Dir: f.dir, AccountKey: f.accountKey, Directory: f.directory, HTTPClient: client, DNS: provider,
		RenewBefore: f.renewBefore, Propagation: f.propagation, CheckEvery: f.checkEvery,
		SeedCert: seedCert, SeedKey: seedKey, Logf: logf,
	})
}

// runTURNCertRenewer keeps the certificate valid and opens no listener: what
// it holds (the DNS credential, the ACME account key) is reachable from no
// network. It reports through status.json, which the TURN server serves on
// its health and metrics.
func runTURNCertRenewer(f turnCertFlags, seedCert, seedKey string) error {
	keeper, err := f.keeper(seedCert, seedKey, log.Printf)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	go func() {
		for range hup {
			log.Printf("turn-cert: SIGHUP: renewing now")
			keeper.RenewNow()
		}
	}()
	log.Printf("mewla-relay ready: TURN certificate renewer for %s into %s (no listener)", f.domain, f.dir)
	keeper.Run(ctx)
	return nil
}

// turnOperatorHandler is a TURN-only process's health and metrics: whether
// it serves, its allocations, the days until its TLS certificate expires and
// the renewer's report.
func turnOperatorHandler(srv *relay.TURNServer, certs *acmecert.Watcher) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		body := map[string]any{"status": "ok"}
		code := http.StatusOK
		if certs != nil {
			st := certs.Status()
			now := time.Now()
			cert := map[string]any{
				"domain": st.Domain, "days_left": roundDays(st.DaysLeft(now)),
				"renewal_failures": st.Failures,
			}
			if !st.NotAfter.IsZero() {
				cert["not_after"] = st.NotAfter.UTC().Format(time.RFC3339)
			}
			if !st.Renewed.IsZero() {
				cert["renewed"] = st.Renewed.UTC().Format(time.RFC3339)
			}
			if !st.Checked.IsZero() {
				cert["renewer_checked"] = st.Checked.UTC().Format(time.RFC3339)
			}
			if st.LastError != "" {
				cert["last_error"] = st.LastError
			}
			body["tls_certificate"] = cert
			if st.NotAfter.IsZero() || !now.Before(st.NotAfter) {
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
		if certs != nil {
			st := certs.Status()
			fmt.Fprintf(w, "# TYPE mewla_turn_tls_certificate_days_left gauge\nmewla_turn_tls_certificate_days_left %.2f\n", st.DaysLeft(time.Now()))
			fmt.Fprintf(w, "# TYPE mewla_turn_tls_certificate_not_after_seconds gauge\nmewla_turn_tls_certificate_not_after_seconds %d\n", unixOrZero(st.NotAfter))
			fmt.Fprintf(w, "# TYPE mewla_turn_tls_renewal_failures gauge\nmewla_turn_tls_renewal_failures %d\n", st.Failures)
			fmt.Fprintf(w, "# TYPE mewla_turn_tls_renewer_checked_seconds gauge\nmewla_turn_tls_renewer_checked_seconds %d\n", unixOrZero(st.Checked))
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
