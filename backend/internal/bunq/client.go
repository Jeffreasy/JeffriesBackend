package bunq

import (
	"bytes"
	"context"
	"crypto"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Config struct {
	Environment       string
	APIKey            string
	DeviceDescription string
}

type Introspection struct {
	Environment      string    `json:"environment"`
	UserID           int       `json:"userId"`
	UserType         string    `json:"userType"`
	PrimaryAccountID *int      `json:"primaryAccountId,omitempty"`
	Accounts         []Account `json:"accounts"`
}

type Account struct {
	ID          int    `json:"id"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Currency    string `json:"currency,omitempty"`
	Status      string `json:"status,omitempty"`
	UserID      int    `json:"userId,omitempty"`
	IBANMasked  string `json:"ibanMasked,omitempty"`
}

type Client struct {
	baseURL    string
	userAgent  string
	httpClient *http.Client
	privateKey *rsa.PrivateKey
}

func Discover(ctx context.Context, cfg Config) (*Introspection, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, errors.New("BUNQ_API_KEY ontbreekt")
	}

	privateKey, err := rsa.GenerateKey(crand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("RSA key maken mislukt: %w", err)
	}

	env := normalizeEnvironment(cfg.Environment)
	client := &Client{
		baseURL:    baseURL(env),
		userAgent:  "JeffriesHomeapp/1.0 " + env,
		httpClient: &http.Client{Timeout: 20 * time.Second},
		privateKey: privateKey,
	}

	publicKey, err := publicKeyPEM(privateKey)
	if err != nil {
		return nil, err
	}

	installation, err := client.do(ctx, http.MethodPost, "/installation", "", map[string]string{
		"client_public_key": publicKey,
	}, false)
	if err != nil {
		return nil, fmt.Errorf("installation: %w", err)
	}
	installationToken, ok := findToken(installation)
	if !ok {
		return nil, errors.New("installation token ontbreekt in bunq response")
	}

	description := strings.TrimSpace(cfg.DeviceDescription)
	if description == "" {
		description = "JeffriesHomeapp Render"
	}
	if _, err := client.do(ctx, http.MethodPost, "/device-server", installationToken, map[string]string{
		"description": description,
		"secret":      apiKey,
	}, true); err != nil {
		return nil, fmt.Errorf("device-server: %w", err)
	}

	session, err := client.do(ctx, http.MethodPost, "/session-server", installationToken, map[string]string{
		"secret": apiKey,
	}, true)
	if err != nil {
		return nil, fmt.Errorf("session-server: %w", err)
	}
	sessionToken, ok := findToken(session)
	if !ok {
		return nil, errors.New("session token ontbreekt in bunq response")
	}

	userID, userType, ok := findUser(session)
	if !ok {
		user, err := client.do(ctx, http.MethodGet, "/user", sessionToken, nil, false)
		if err != nil {
			return nil, fmt.Errorf("user ophalen: %w", err)
		}
		userID, userType, ok = findUser(user)
		if !ok {
			return nil, errors.New("user id ontbreekt in bunq response")
		}
	}

	accountsEnvelope, err := client.do(ctx, http.MethodGet, fmt.Sprintf("/user/%d/monetary-account-bank", userID), sessionToken, nil, false)
	if err != nil {
		return nil, fmt.Errorf("monetary-account-bank ophalen: %w", err)
	}
	accounts := findAccounts(accountsEnvelope)

	var primary *int
	for _, account := range accounts {
		if account.Status == "" || strings.EqualFold(account.Status, "ACTIVE") {
			id := account.ID
			primary = &id
			break
		}
	}
	if primary == nil && len(accounts) > 0 {
		id := accounts[0].ID
		primary = &id
	}

	return &Introspection{
		Environment:      env,
		UserID:           userID,
		UserType:         userType,
		PrimaryAccountID: primary,
		Accounts:         accounts,
	}, nil
}

func (c *Client) do(ctx context.Context, method, path, token string, body any, signed bool) (*envelope, error) {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("X-Bunq-Language", "nl_NL")
	req.Header.Set("X-Bunq-Region", "nl_NL")
	req.Header.Set("X-Bunq-Geolocation", "0 0 0 0 NL")
	req.Header.Set("X-Bunq-Client-Request-Id", requestID())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Bunq-Client-Authentication", token)
	}
	if signed {
		signature, err := c.sign(payload)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Bunq-Client-Signature", signature)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: %s", method, path, bunqError(raw, resp.StatusCode))
	}

	var out envelope
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("bunq response lezen mislukt: %w", err)
	}
	return &out, nil
}

func (c *Client) sign(payload []byte) (string, error) {
	sum := sha256.Sum256(payload)
	signature, err := rsa.SignPKCS1v15(crand.Reader, c.privateKey, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

type envelope struct {
	Response []map[string]json.RawMessage `json:"Response"`
	Error    []bunqErrorObject            `json:"Error"`
}

type bunqErrorObject struct {
	ErrorDescription           string `json:"error_description"`
	ErrorDescriptionTranslated string `json:"error_description_translated"`
	ErrorCode                  string `json:"error_code"`
}

type tokenObject struct {
	Token string `json:"token"`
}

type idObject struct {
	ID int `json:"id"`
}

type monetaryAccountBank struct {
	ID          int             `json:"id"`
	Description string          `json:"description"`
	DisplayName string          `json:"display_name"`
	Currency    string          `json:"currency"`
	Status      string          `json:"status"`
	UserID      int             `json:"user_id"`
	Alias       []monetaryAlias `json:"alias"`
}

type monetaryAlias struct {
	Type  string `json:"type"`
	Value string `json:"value"`
	Name  string `json:"name"`
}

func publicKeyPEM(privateKey *rsa.PrivateKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", err
	}
	block := &pem.Block{Type: "PUBLIC KEY", Bytes: der}
	return string(pem.EncodeToMemory(block)), nil
}

func findToken(env *envelope) (string, bool) {
	for _, item := range env.Response {
		raw, ok := item["Token"]
		if !ok {
			continue
		}
		var token tokenObject
		if err := json.Unmarshal(raw, &token); err == nil && token.Token != "" {
			return token.Token, true
		}
	}
	return "", false
}

func findUser(env *envelope) (int, string, bool) {
	for _, item := range env.Response {
		for _, key := range []string{"UserPerson", "UserCompany", "UserPaymentServiceProvider"} {
			raw, ok := item[key]
			if !ok {
				continue
			}
			var id idObject
			if err := json.Unmarshal(raw, &id); err == nil && id.ID > 0 {
				return id.ID, key, true
			}
		}
	}
	return 0, "", false
}

func findAccounts(env *envelope) []Account {
	accounts := make([]Account, 0, len(env.Response))
	for _, item := range env.Response {
		raw, ok := item["MonetaryAccountBank"]
		if !ok {
			continue
		}
		var account monetaryAccountBank
		if err := json.Unmarshal(raw, &account); err != nil || account.ID <= 0 {
			continue
		}
		accounts = append(accounts, Account{
			ID:          account.ID,
			Type:        "MonetaryAccountBank",
			Description: account.Description,
			DisplayName: account.DisplayName,
			Currency:    account.Currency,
			Status:      account.Status,
			UserID:      account.UserID,
			IBANMasked:  maskedIBAN(account.Alias),
		})
	}
	return accounts
}

func bunqError(raw []byte, status int) string {
	var env envelope
	if err := json.Unmarshal(raw, &env); err == nil && len(env.Error) > 0 {
		parts := make([]string, 0, len(env.Error))
		for _, item := range env.Error {
			text := strings.TrimSpace(item.ErrorDescriptionTranslated)
			if text == "" {
				text = strings.TrimSpace(item.ErrorDescription)
			}
			if text == "" {
				text = strings.TrimSpace(item.ErrorCode)
			}
			if text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
	}
	return fmt.Sprintf("bunq HTTP %d", status)
}

func maskedIBAN(aliases []monetaryAlias) string {
	for _, alias := range aliases {
		if !strings.EqualFold(alias.Type, "IBAN") || alias.Value == "" {
			continue
		}
		value := strings.ReplaceAll(alias.Value, " ", "")
		if len(value) <= 8 {
			return "****"
		}
		return value[:4] + "..." + value[len(value)-4:]
	}
	return ""
}

func normalizeEnvironment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "production" || value == "prod" || value == "live" {
		return "production"
	}
	return "sandbox"
}

func baseURL(environment string) string {
	if environment == "production" {
		return "https://api.bunq.com/v1"
	}
	return "https://public-api.sandbox.bunq.com/v1"
}

func requestID() string {
	var random [8]byte
	if _, err := crand.Read(random[:]); err != nil {
		return fmt.Sprintf("homeapp-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("homeapp-%d-%s", time.Now().UnixNano(), hex.EncodeToString(random[:]))
}
