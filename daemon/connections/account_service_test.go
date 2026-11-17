package connections

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/daoleno/mewla/daemon/slacktoken"
)

// fakeAccountService stands in for the account service's Slack exchange: it
// posts the grant it is given to Slack through the manager's test transport,
// as the service would with Mewla's secret, and records each call.
type fakeAccountService struct {
	m        *Manager
	signedIn bool
	calls    []string
	err      error // the service's own failure, when set
}

func (f *fakeAccountService) SignedIn() bool { return f.signedIn }

func (f *fakeAccountService) SlackToken(ctx context.Context, path string, body any) (json.RawMessage, error) {
	f.calls = append(f.calls, path)
	if f.err != nil {
		return nil, f.err
	}
	form := url.Values{}
	switch b := body.(type) {
	case slacktoken.Exchange:
		form = url.Values{"grant_type": {"authorization_code"}, "code": {b.Code}, "code_verifier": {b.CodeVerifier}, "redirect_uri": {b.RedirectURI}}
	case slacktoken.Refresh:
		form = url.Values{"grant_type": {"refresh_token"}, "refresh_token": {b.RefreshToken}}
	default:
		return nil, errors.New("unexpected body")
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, slacktoken.TokenURL, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := f.m.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("slack_unreachable")
	}
	raw, err := io.ReadAll(response.Body)
	return raw, err
}

// signedInSlack gives m a signed-in account service that reaches Slack
// through m's test transport.
func signedInSlack(m *Manager) *fakeAccountService {
	f := &fakeAccountService{m: m, signedIn: true}
	m.SetAccountService(f)
	return f
}
