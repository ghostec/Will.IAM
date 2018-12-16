package oauth2

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ghostec/Will.IAM/models"
	"github.com/ghostec/Will.IAM/repositories"
)

const tokenEndpoint = "https://www.googleapis.com/oauth2/v4/token"
const userEndpoint = "https://www.googleapis.com/oauth2/v2/userinfo"

// GoogleConfig are the basic required informations to use Google
// as oauth2 provider
type GoogleConfig struct {
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	HostedDomains []string
}

var googleConfig GoogleConfig

func buildURL(endpoint, queryStrings string) string {
	return fmt.Sprintf("%s?%s", endpoint, queryStrings)
}

func mapToQueryStrings(m map[string]string) string {
	s := []string{}
	for k, v := range m {
		s = append(s, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(s, "&")
}

// Google implements Provider
type Google struct {
	config           GoogleConfig
	tokensRepository repositories.Tokens
	client           *http.Client
}

// BuildAuthURL returns an URL authenticate with Google
func (g *Google) BuildAuthURL(state string) string {
	qs := mapToQueryStrings(map[string]string{
		"state":        state,
		"redirect_uri": g.config.RedirectURL,
		"client_id":    g.config.ClientID,
		"scope": strings.Join([]string{
			url.QueryEscape("https://www.googleapis.com/auth/userinfo.profile"),
			url.QueryEscape("https://www.googleapis.com/auth/userinfo.email"),
		}, "+"),
		"access_type":            "offline",
		"include_granted_scopes": "true",
		"response_type":          "code",
		"prompt":                 "consent",
	})
	return buildURL("https://accounts.google.com/o/oauth2/v2/auth", qs)
}

func (g *Google) buildExchangeCodeForm(code string) string {
	v := url.Values{}
	v.Add("code", code)
	v.Add("client_id", g.config.ClientID)
	v.Add("client_secret", g.config.ClientSecret)
	v.Add("redirect_uri", g.config.RedirectURL)
	v.Add("grant_type", "authorization_code")
	return v.Encode()
}

// ExchangeCode will trade code for full token with Google
func (g *Google) ExchangeCode(code string) (*AuthResult, error) {
	t, err := g.tokenFromCode(code)
	if err != nil {
		return nil, err
	}
	userInfo, err := g.getUserInfo(t.AccessToken)
	if err != nil {
		return nil, err
	}
	allowed := g.checkHostedDomain(userInfo.HostedDomain)
	if !allowed {
		return nil, fmt.Errorf(
			"email from non-allowed hosted domain %s", userInfo.HostedDomain,
		)
	}
	t.Email = userInfo.Email
	if err := g.tokensRepository.Save(t); err != nil {
		return nil, err
	}
	return &AuthResult{
		AccessToken: t.AccessToken,
		Email:       t.Email,
	}, nil
}

func (g *Google) tokenFromCode(code string) (*models.Token, error) {
	ecf := g.buildExchangeCodeForm(code)
	req, err := http.NewRequest("POST", tokenEndpoint, strings.NewReader(ecf))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	if err != nil {
		return nil, err
	}
	res, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := ioutil.ReadAll(res.Body)
	tmap := map[string]interface{}{}
	err = json.Unmarshal(body, &tmap)
	if err != nil {
		return nil, err
	}
	return &models.Token{
		AccessToken:  tmap["access_token"].(string),
		RefreshToken: tmap["refresh_token"].(string),
		TokenType:    tmap["token_type"].(string),
		Expiry: time.Now().Add(
			time.Second * time.Duration(tmap["expires_in"].(float64)),
		),
	}, nil
}

type userInfo struct {
	Email        string `json:"email"`
	HostedDomain string `json:"hd"`
}

func (g *Google) getUserInfo(accessToken string) (*userInfo, error) {
	req, err := http.NewRequest("GET", userEndpoint, nil)
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	if err != nil {
		return nil, err
	}
	res, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := ioutil.ReadAll(res.Body)
	ui := &userInfo{}
	err = json.Unmarshal(body, ui)
	if err != nil {
		return nil, err
	}
	return ui, nil
}

func (g *Google) checkHostedDomain(hd string) bool {
	if g.config.HostedDomains == nil || len(g.config.HostedDomains) == 0 {
		return true
	}
	for _, allowed := range g.config.HostedDomains {
		if hd == allowed {
			return true
		}
	}
	return false
}

// Authenticate verifies if an accessToken is valid and maybe refresh it
func (g *Google) Authenticate(accessToken string) (*AuthResult, error) {
	t, err := g.tokensRepository.Get(accessToken)
	if t == nil {
		return nil, fmt.Errorf("access token not found")
	}
	if err != nil {
		return nil, err
	}
	_, err = g.getUserInfo(t.AccessToken)
	if err != nil {
		return nil, err
	}
	// TODO: handle refresh
	return &AuthResult{
		AccessToken: t.AccessToken,
		Email:       t.Email,
	}, nil
}

// NewGoogle ctor
func NewGoogle(
	config GoogleConfig, tokensRepository repositories.Tokens,
) *Google {
	return &Google{
		config:           config,
		tokensRepository: tokensRepository,
		client:           &http.Client{},
	}
}