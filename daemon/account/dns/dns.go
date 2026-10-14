// Package dns publishes ACME DNS-01 TXT records for machine hostnames.
package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Provider sets and clears one TXT record value. fqdn is given without a
// trailing dot.
type Provider interface {
	SetTXT(ctx context.Context, fqdn, value string) error
	ClearTXT(ctx context.Context, fqdn, value string) error
}

const maxResponseBytes = 1 << 20

func defaultClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Challtestsrv drives pebble-challtestsrv's management API (development).
type Challtestsrv struct {
	BaseURL string
	Client  *http.Client
}

func (c *Challtestsrv) SetTXT(ctx context.Context, fqdn, value string) error {
	return c.post(ctx, "/set-txt", map[string]string{
		"host":  dnsName(fqdn),
		"value": value,
	})
}

func (c *Challtestsrv) ClearTXT(ctx context.Context, fqdn, _ string) error {
	return c.post(ctx, "/clear-txt", map[string]string{"host": dnsName(fqdn)})
}

func (c *Challtestsrv) post(ctx context.Context, path string, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		strings.TrimRight(c.BaseURL, "/")+path,
		bytes.NewReader(raw),
	)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := defaultClient(c.Client).Do(request)
	if err != nil {
		return fmt.Errorf("challtestsrv %s: %w", path, err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
	if response.StatusCode/100 != 2 {
		return fmt.Errorf("challtestsrv %s returned %s", path, response.Status)
	}
	return nil
}

// Cloudflare manages TXT records in one zone through the v4 REST API.
type Cloudflare struct {
	ZoneID  string
	Token   string
	BaseURL string
	Client  *http.Client
}

const cloudflareBaseURL = "https://api.cloudflare.com"

type cloudflareRecord struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl,omitempty"`
}

type cloudflareResponse struct {
	Success bool            `json:"success"`
	Errors  []cloudflareMsg `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type cloudflareMsg struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Cloudflare) SetTXT(ctx context.Context, fqdn, value string) error {
	record := cloudflareRecord{
		Type:    "TXT",
		Name:    strings.TrimSuffix(fqdn, "."),
		Content: value,
		TTL:     60,
	}
	_, err := c.do(ctx, http.MethodPost, c.recordsPath(), record)
	return err
}

func (c *Cloudflare) ClearTXT(ctx context.Context, fqdn, value string) error {
	query := url.Values{}
	query.Set("type", "TXT")
	query.Set("name", strings.TrimSuffix(fqdn, "."))
	query.Set("content", value)
	result, err := c.do(ctx, http.MethodGet, c.recordsPath()+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	var records []cloudflareRecord
	if err := json.Unmarshal(result, &records); err != nil {
		return errors.New("cloudflare: malformed record list")
	}
	for _, record := range records {
		if record.ID == "" || record.Content != value {
			continue
		}
		if _, err := c.do(
			ctx,
			http.MethodDelete,
			c.recordsPath()+"/"+url.PathEscape(record.ID),
			nil,
		); err != nil {
			return err
		}
	}
	return nil
}

func (c *Cloudflare) recordsPath() string {
	return "/client/v4/zones/" + url.PathEscape(c.ZoneID) + "/dns_records"
}

func (c *Cloudflare) do(
	ctx context.Context,
	method string,
	path string,
	body any,
) (json.RawMessage, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	base := c.BaseURL
	if base == "" {
		base = cloudflareBaseURL
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.Token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := defaultClient(c.Client).Do(request)
	if err != nil {
		return nil, fmt.Errorf("cloudflare %s dns_records: %w", method, err)
	}
	defer response.Body.Close()
	var decoded cloudflareResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("cloudflare %s dns_records returned %s", method, response.Status)
	}
	if response.StatusCode/100 != 2 || !decoded.Success {
		if len(decoded.Errors) > 0 {
			return nil, fmt.Errorf(
				"cloudflare %s dns_records: %d %s",
				method,
				decoded.Errors[0].Code,
				decoded.Errors[0].Message,
			)
		}
		return nil, fmt.Errorf("cloudflare %s dns_records returned %s", method, response.Status)
	}
	return decoded.Result, nil
}

func dnsName(fqdn string) string {
	return strings.TrimSuffix(fqdn, ".") + "."
}
