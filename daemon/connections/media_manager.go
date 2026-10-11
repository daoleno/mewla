package connections

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
)

// API-key media accounts: discovery, key replacement, and calls that run
// outside the manager lock, because a generation can take minutes.

func mediaTools(kind string) []Tool {
	if kind == "fal" {
		return falTools()
	}
	return imageTools()
}

// discoverMedia lists the reviewed tools and checks the key. A fal account
// may exist without a key: its free catalogue tools work, everything else
// asks for a key.
func (m *Manager) discoverMedia(ctx context.Context, r *record, secret string) error {
	r.Account.AuthMethod = "api_key"
	r.Account.Tools = mediaTools(r.Account.Integration)
	call := m.mediaCall(ctx, r, secret, "", mediaShortTimeout)
	now := time.Now().UTC()
	if r.Account.Integration == "fal" {
		if secret == "" {
			r.Account.Identity = "No API key yet"
			r.Account.Status = "authorization_required"
			r.Account.VerifiedAt = nil
			return nil
		}
		if err := call.falVerify(ctx); err != nil {
			return err
		}
		r.Account.Identity = "API key"
		r.Account.Status = "connected"
		r.Account.VerifiedAt = &now
		return nil
	}
	verified, err := call.imageVerify(ctx)
	if err != nil {
		return err
	}
	host := ""
	if u, err := url.Parse(r.Account.Endpoint); err == nil {
		host = u.Host
	}
	r.Account.Identity = host + " · " + r.Account.Model
	if verified {
		r.Account.Status = "connected"
		r.Account.VerifiedAt = &now
	} else if r.Account.VerifiedAt == nil {
		// The endpoint lists no models; the first generation verifies it.
		r.Account.Status = "configured"
	}
	return nil
}

// setKey replaces an API-key account's key once the service accepts it.
func (m *Manager) setKey(ctx context.Context, r *record, in *Input) error {
	if !mediaKind(r.Account.Integration) {
		return errors.New("only API-key services take a key")
	}
	if r.Account.Status == "disconnected" {
		return errors.New("connect this account again")
	}
	key := ""
	if in != nil {
		key = strings.TrimSpace(in.Credential)
	}
	if key == "" || len(key) > 16<<10 || strings.ContainsAny(key, "\r\n") {
		return errors.New("enter the API key")
	}
	if err := m.discover(ctx, r, key); err != nil {
		return err
	}
	if err := m.vault.Set("integration:"+r.Account.ID, key); err != nil {
		return errors.New("credential could not be stored")
	}
	return nil
}

// invokeMedia runs one media call. The manager lock is held only to check
// the call and to record its outcome. handled is false for other accounts.
func (m *Manager) invokeMedia(parent context.Context, q Request) (response Response, handled bool, err error) {
	m.mu.Lock()
	r, ok := m.records[q.ID]
	if !ok || !mediaKind(r.Account.Integration) || m.isPending(q.ID) {
		m.mu.Unlock()
		return Response{}, false, nil
	}
	call, tool, args, err := m.prepareMedia(parent, r, q)
	m.mu.Unlock()
	if err != nil {
		return Response{}, true, err
	}
	ctx, cancel := context.WithTimeout(parent, mediaTimeout(tool))
	defer cancel()
	var result []byte
	if call.account.Integration == "fal" {
		result, err = call.invokeFal(ctx, tool, args)
	} else {
		result, err = call.invokeImage(ctx, tool, args)
	}
	if ctx.Err() != nil {
		err = safeError(ctx.Err())
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result, err = m.finishMedia(q.ID, tool, call.secret, result, err)
	if err != nil {
		return Response{}, true, err
	}
	return Response{Result: result}, true, nil
}

func (m *Manager) prepareMedia(ctx context.Context, r *record, q Request) (*mediaCall, string, map[string]any, error) {
	secret, err := m.accountToken(ctx, r)
	if err != nil {
		return nil, "", nil, err
	}
	free := r.Account.Integration == "fal" && (q.Tool == "search_models" || q.Tool == "describe_model")
	if secret == "" && !free && r.Account.Status != "disconnected" {
		// The missing key is the useful answer, before any grant.
		return nil, "", nil, keyError{serviceName(r.Account.Integration), true}
	}
	t, args, err := checkInvoke(r, q)
	if err != nil {
		return nil, "", nil, err
	}
	reviewed := false
	for _, candidate := range mediaTools(r.Account.Integration) {
		reviewed = reviewed || fingerprint(candidate) == fingerprint(t)
	}
	if !reviewed {
		return nil, "", nil, errors.New("tool definition changed; refresh and review authorization")
	}
	cwd := ""
	if isLocalCaller(ctx) {
		cwd = q.Cwd
	}
	return m.mediaCall(ctx, r, secret, cwd, mediaTimeout(t.Name)), t.Name, args, nil
}

// finishMedia records a finished call on its account, looked up again: the
// account may have been changed or disconnected while the call ran.
func (m *Manager) finishMedia(id, tool, secret string, result []byte, err error) ([]byte, error) {
	if err == nil && !json.Valid(result) {
		err = errors.New("integration returned invalid JSON")
	}
	if err == nil && len(result) > MaxResultBytes {
		err = errors.New("integration result exceeds 1 MiB")
	}
	r := m.records[id]
	if r == nil {
		return result, err
	}
	if err == nil {
		result = redactValues(result, m.sensitiveValues(r, secret))
	}
	var vendor vendorError
	switch {
	case r.Account.Status == "disconnected":
	case errors.Is(err, errAuth):
		r.Account.Status = "authorization_required"
	case errors.As(err, &vendor):
		// The service answered: usually the input, not the account.
	case err != nil:
		if secret != "" {
			r.Account.Status = "error"
		}
	case secret != "":
		r.Account.Status = "connected"
		now := time.Now().UTC()
		r.Account.VerifiedAt = &now
	}
	history := err
	if errors.As(err, &vendor) {
		history = errors.New(vendor.short)
	}
	m.event(r, tool, history)
	if saveErr := m.save(); saveErr != nil {
		return nil, errors.New("call finished but history could not be saved; inspect before retrying")
	}
	return result, err
}
