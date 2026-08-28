package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type discovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserInfoEndpoint      string `json:"userinfo_endpoint"`
	RevocationEndpoint    string `json:"revocation_endpoint"`
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: go run ./examples/go-client <discovery|authorize|exchange|userinfo|revoke> ...")
	}
	issuer := strings.TrimRight(os.Getenv("CONNECT_ISSUER_URL"), "/")
	if issuer == "" {
		log.Fatal("CONNECT_ISSUER_URL is required")
	}
	d, err := loadDiscovery(issuer)
	if err != nil {
		log.Fatal(err)
	}
	switch os.Args[1] {
	case "discovery":
		printJSON(d)
	case "authorize":
		printAuthorize(d.AuthorizationEndpoint)
	case "exchange":
		if len(os.Args) != 4 {
			log.Fatal("exchange requires <code> <code-verifier>")
		}
		exchange(d, os.Args[2], os.Args[3])
	case "userinfo":
		if len(os.Args) != 3 {
			log.Fatal("userinfo requires <access-token>")
		}
		requestJSON(http.MethodGet, d.UserInfoEndpoint, os.Args[2], "")
	case "revoke":
		if len(os.Args) != 3 {
			log.Fatal("revoke requires <token>")
		}
		revoke(d.RevocationEndpoint, os.Args[2])
	default:
		log.Fatalf("unknown command %q", os.Args[1])
	}
}

func loadDiscovery(issuer string) (discovery, error) {
	response, err := http.Get(issuer + "/.well-known/openid-configuration")
	if err != nil {
		return discovery{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return discovery{}, fmt.Errorf("discovery returned HTTP %d", response.StatusCode)
	}
	var result discovery
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return discovery{}, err
	}
	return result, nil
}

func printAuthorize(endpoint string) {
	clientID := os.Getenv("CONNECT_CLIENT_ID")
	redirectURI := os.Getenv("CONNECT_REDIRECT_URI")
	if clientID == "" || redirectURI == "" {
		log.Fatal("CONNECT_CLIENT_ID and CONNECT_REDIRECT_URI are required")
	}
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		log.Fatal(err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	stateBytes := make([]byte, 24)
	nonceBytes := make([]byte, 24)
	if _, err := rand.Read(stateBytes); err != nil {
		log.Fatal(err)
	}
	if _, err := rand.Read(nonceBytes); err != nil {
		log.Fatal(err)
	}
	values := url.Values{
		"client_id": {clientID}, "redirect_uri": {redirectURI}, "response_type": {"code"},
		"scope": {"openid profile community"}, "state": {base64.RawURLEncoding.EncodeToString(stateBytes)},
		"nonce": {base64.RawURLEncoding.EncodeToString(nonceBytes)}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	fmt.Println(endpoint + "?" + values.Encode())
	fmt.Println("save this verifier for the exchange:", verifier)
}

func exchange(d discovery, code, verifier string) {
	clientID, clientSecret, redirectURI := os.Getenv("CONNECT_CLIENT_ID"), os.Getenv("CONNECT_CLIENT_SECRET"), os.Getenv("CONNECT_REDIRECT_URI")
	if clientID == "" || clientSecret == "" || redirectURI == "" {
		log.Fatal("CONNECT_CLIENT_ID, CONNECT_CLIENT_SECRET and CONNECT_REDIRECT_URI are required")
	}
	values := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}, "code_verifier": {verifier}}
	request, err := http.NewRequest(http.MethodPost, d.TokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		log.Fatal(err)
	}
	request.SetBasicAuth(clientID, clientSecret)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		log.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	fmt.Printf("HTTP %d\n%s\n", response.StatusCode, data)
}

func requestJSON(method, endpoint, token, body string) {
	request, _ := http.NewRequest(method, endpoint, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		log.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	fmt.Printf("HTTP %d\n%s\n", response.StatusCode, data)
}

func revoke(endpoint, token string) {
	request, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(url.Values{"token": {token}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		log.Fatal(err)
	}
	defer response.Body.Close()
	fmt.Println("revoke HTTP", response.StatusCode)
}

func printJSON(value any) {
	encoded, _ := json.MarshalIndent(value, "", "  ")
	fmt.Println(string(encoded))
}
