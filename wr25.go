// Package eveonline implements the OAuth2 protocol for authenticating users through eveonline.
// This package can be used as a reference implementation of an OAuth2 provider for Goth.
package oauthwr25provider

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"fmt"

	"github.com/gorilla/sessions"
	"github.com/markbates/goth"
	"golang.org/x/oauth2"
)

const (
	authPath   string = "https://auth.wr25.org/oauth/authorize/"
	tokenPath  string = "https://auth.wr25.org/oauth/token"
	verifyPath string = "https://auth.wr25.org/oauth/verify"
)

type UserData struct {
	user goth.User
}

func GetUser(store *sessions.CookieStore, r *http.Request) (UserData, error) {
	data := UserData{}
	session, err := store.Get(r, "user")
	if err != nil {
		return data, fmt.Errorf("No sessions found")
	}

	user, ok := session.Values["user"].(goth.User)
	if !ok {
		return data, fmt.Errorf("Session hasn't any user stored..")
	}

	data.user = user
	return data, nil
}

func (ud *UserData) UserIn(group string) (bool, error) {
	groups, err := ud.Groups()
	if err != nil {
		return false, err
	}

	for _, g := range groups {
		if g == group {
			return true, nil
		}
	}
	return false, nil
}

func (ud *UserData) Groups() ([]string, error) {
	req, err := http.NewRequest("GET", verifyPath, nil)
	if err != nil {
		return []string{}, err
	}

	req.Header.Add("Authorization", "Bearer "+ud.user.AccessToken)

	client := &http.Client{}

	resp, err := client.Do(req)

	if err != nil {
		return []string{}, fmt.Errorf("Unable to do teh request")
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return []string{}, fmt.Errorf("Not authorized, most likely the token has timed out ...")
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return []string{}, fmt.Errorf("Couldn't read the body")
	}

	defer resp.Body.Close()

	type groups struct {
		Groups []string `json:"groups"`
	}

	g := groups{}
	err = json.Unmarshal(data, &g)
	if err != nil {
		return []string{}, fmt.Errorf("Couldn't unmarshal response")
	}
	return g.Groups, nil
}

// Provider is the implementation of `goth.Provider` for accessing eveonline.
type Provider struct {
	ClientKey    string
	Secret       string
	CallbackURL  string
	HTTPClient   *http.Client
	config       *oauth2.Config
	providerName string
}

// New creates a new Eve Online provider and sets up important connection details.
// You should always call `eveonline.New` to get a new provider.  Never try to
// create one manually.
func New(clientKey, secret, callbackURL string, scopes ...string) *Provider {
	p := &Provider{
		ClientKey:    clientKey,
		Secret:       secret,
		CallbackURL:  callbackURL,
		providerName: "authwr25",
	}
	p.config = newConfig(p, scopes)
	return p
}

// Name is the name used to retrieve this provider later.
func (p *Provider) Name() string {
	return p.providerName
}

// SetName is to update the name of the provider (needed in case of multiple providers of 1 type)
func (p *Provider) SetName(name string) {
	p.providerName = name
}

// Client returns the default http.client
func (p *Provider) Client() *http.Client {
	return goth.HTTPClientWithFallBack(p.HTTPClient)
}

// Debug is a no-op for the eveonline package.
func (p *Provider) Debug(debug bool) {}

// BeginAuth asks Eve Online for an authentication end-point.
func (p *Provider) BeginAuth(state string) (goth.Session, error) {
	return &Session{
		AuthURL: p.config.AuthCodeURL(state),
	}, nil
}

// FetchUser will go to Eve Online and access basic information about the user.
func (p *Provider) FetchUser(session goth.Session) (goth.User, error) {
	sess := session.(*Session)
	user := goth.User{
		AccessToken:  sess.AccessToken,
		Provider:     p.Name(),
		RefreshToken: sess.RefreshToken,
		ExpiresAt:    sess.ExpiresAt,
	}

	if user.AccessToken == "" {
		// data is not yet retrieved since accessToken is still empty
		return user, fmt.Errorf("%s cannot get user information without accessToken", p.providerName)
	}

	// Get the userID, eveonline needs userID in order to get user profile info
	req, err := http.NewRequest("GET", verifyPath, nil)
	if err != nil {
		return user, err
	}
	req.Header.Set("Authorization", "Bearer "+user.AccessToken)

	response, err := p.Client().Do(req)
	if err != nil {
		if response != nil {
			response.Body.Close()
		}
		return user, err
	}

	if response.StatusCode != http.StatusOK {
		return user, fmt.Errorf("%s responded with a %d trying to fetch user information", p.providerName, response.StatusCode)
	}

	bits, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return user, err
	}

	u := struct {
		CharacterID        int64
		CharacterName      string
		ExpiresOn          string
		Scopes             string
		TokenType          string
		CharacterOwnerHash string
	}{}

	if err = json.NewDecoder(bytes.NewReader(bits)).Decode(&u); err != nil {
		return user, err
	}

	user.NickName = u.CharacterName
	user.UserID = fmt.Sprintf("%d", u.CharacterID)
	return user, err
}

func newConfig(provider *Provider, scopes []string) *oauth2.Config {
	c := &oauth2.Config{
		ClientID:     provider.ClientKey,
		ClientSecret: provider.Secret,
		RedirectURL:  provider.CallbackURL,
		Endpoint: oauth2.Endpoint{
			AuthURL:  authPath,
			TokenURL: tokenPath,
		},
		Scopes: []string{},
	}

	if len(scopes) > 0 {
		for _, scope := range scopes {
			c.Scopes = append(c.Scopes, scope)
		}
	}
	return c
}

//RefreshTokenAvailable refresh token is provided by auth provider or not
func (p *Provider) RefreshTokenAvailable() bool {
	return true
}

//RefreshToken get new access token based on the refresh token
func (p *Provider) RefreshToken(refreshToken string) (*oauth2.Token, error) {
	token := &oauth2.Token{RefreshToken: refreshToken}
	ts := p.config.TokenSource(goth.ContextForClient(p.Client()), token)
	newToken, err := ts.Token()
	if err != nil {
		return nil, err
	}
	return newToken, err
}
