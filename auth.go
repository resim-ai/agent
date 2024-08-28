package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
	"golang.org/x/oauth2"
)

const (
	devClientID = "xJv0jqeP7QdPOsUidorgDlj4Mi74gVEW"
	audience    = "https://api.resim.ai"
)

type AuthMode string

const (
	authModePassword AuthMode = "password"
	authModeRefresh  AuthMode = "refresh"
)

type tokenJSON struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int32  `json:"expires_in"`
}

func (a *Agent) checkAuth() error {
	a.loadCredentialCache()

	if time.Now().After(a.Token.Expiry.Add(-10*time.Second)) && a.Token.RefreshToken != "" {
		a.authenticate(authModeRefresh)
		a.saveCredentialCache()
	} else if !(a.Token.Valid()) {
		a.authenticate(authModePassword)
		a.saveCredentialCache()
	}

	return nil
}

func (a *Agent) authenticate(mode AuthMode) {

	tokenURL := fmt.Sprintf("%v/oauth/token", a.AuthHost)
	username := viper.GetString(UsernameKey)
	password := viper.GetString(PasswordKey)
	var payloadVals url.Values

	switch mode {

	case authModePassword:
		payloadVals = url.Values{
			"grant_type": []string{"http://auth0.com/oauth/grant-type/password-realm"},
			"realm":      []string{"agents"},
			"username":   []string{username},
			"password":   []string{password},
			"audience":   []string{audience},
			"client_id":  []string{a.ClientID},
			"scope":      []string{"offline_access"},
		}
	case authModeRefresh:
		payloadVals = url.Values{
			"grant_type":    []string{"refresh_token"},
			"client_id":     []string{a.ClientID},
			"refresh_token": []string{a.Token.RefreshToken},
		}
	}

	req, _ := http.NewRequest("POST", tokenURL, strings.NewReader(payloadVals.Encode()))

	req.Header.Add("content-type", "application/x-www-form-urlencoded")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal("error in auth: ", err)
	}

	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	var tj tokenJSON
	err = json.Unmarshal(body, &tj)
	if err != nil {
		log.Fatal(err)
	}

	a.Token = &oauth2.Token{
		AccessToken:  tj.AccessToken,
		TokenType:    tj.TokenType,
		RefreshToken: tj.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(tj.ExpiresIn) * time.Second),
	}

	a.saveCredentialCache()
}

func (a *Agent) loadCredentialCache() {
	homedir, _ := os.UserHomeDir()
	path := strings.ReplaceAll(filepath.Join(ConfigPath, CredentialCacheFilename), "$HOME", homedir)

	data, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(data, &a.Token)
	}
}

func (a Agent) saveCredentialCache() {
	data, err := json.Marshal(a.Token)
	if err != nil {
		log.Println("error marshaling credential cache:", err)
		return
	}

	expectedDir, err := GetConfigDir()
	if err != nil {
		return
	}

	path := filepath.Join(expectedDir, CredentialCacheFilename)
	err = os.WriteFile(path, data, 0600)
	if err != nil {
		log.Println("error saving credential cache:", err)
	}
}
