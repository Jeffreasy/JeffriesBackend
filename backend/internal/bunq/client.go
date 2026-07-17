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
	"net/url"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Environment       string
	APIKey            string
	DeviceDescription string
}

type PaymentRequestInput struct {
	UserID            int
	MonetaryAccountID int
	AmountCents       int
	Currency          string
	Description       string
	MerchantReference string
	RedirectURL       string
	IdempotencyKey    string
}

// HTTPError means bunq returned an explicit non-2xx response.
type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("bunq returned %d: %s", e.StatusCode, e.Message)
}

func (e *HTTPError) AmbiguousWrite() bool {
	return e.StatusCode == http.StatusRequestTimeout ||
		e.StatusCode == http.StatusConflict ||
		e.StatusCode == http.StatusLocked ||
		e.StatusCode == http.StatusTooManyRequests ||
		e.StatusCode >= 500
}

// AmbiguousWriteError means the request may have reached bunq but no reliable
// success response was received. Callers must reconcile, never blindly retry.
type AmbiguousWriteError struct{ Err error }

func (e *AmbiguousWriteError) Error() string { return "bunq write result is unknown: " + e.Err.Error() }
func (e *AmbiguousWriteError) Unwrap() error { return e.Err }
func IsAmbiguousWrite(err error) bool {
	var target *AmbiguousWriteError
	return errors.As(err, &target)
}

type PaymentRequest struct {
	ID                int     `json:"id"`
	Status            string  `json:"status,omitempty"`
	AmountValue       string  `json:"amountValue,omitempty"`
	Currency          string  `json:"currency,omitempty"`
	Description       string  `json:"description,omitempty"`
	MerchantReference *string `json:"merchantReference,omitempty"`
	BunqMeShareURL    *string `json:"bunqmeShareUrl,omitempty"`
}

type sessionContext struct {
	client  *Client
	token   string
	userID  int
	userTyp string
}

type sessionCacheEntry struct {
	session   *sessionContext
	expiresAt time.Time
}

var bunqSessionCache = struct {
	sync.Mutex
	items map[string]sessionCacheEntry
}{
	items: make(map[string]sessionCacheEntry),
}

const bunqSessionCacheTTL = 50 * time.Minute

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
	env := normalizeEnvironment(cfg.Environment)
	session, err := authenticate(ctx, cfg)
	if err != nil {
		return nil, err
	}

	accounts, err := listAccountsWithSession(ctx, session,
		fmt.Sprintf("/user/%d/monetary-account-bank?count=200", session.userID))
	if err != nil {
		return nil, fmt.Errorf("monetary-account-bank ophalen: %w", err)
	}

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
		UserID:           session.userID,
		UserType:         session.userTyp,
		PrimaryAccountID: primary,
		Accounts:         accounts,
	}, nil
}

func CreatePaymentRequest(ctx context.Context, cfg Config, input PaymentRequestInput) (*PaymentRequest, error) {
	if input.AmountCents <= 0 {
		return nil, errors.New("bedrag moet groter zijn dan 0")
	}
	if input.MonetaryAccountID <= 0 {
		return nil, errors.New("BUNQ_MONETARY_ACCOUNT_ID ontbreekt")
	}
	session, err := authenticate(ctx, cfg)
	if err != nil {
		return nil, err
	}
	userID := input.UserID
	if userID <= 0 {
		userID = session.userID
	}
	if userID <= 0 {
		return nil, errors.New("BUNQ_USER_ID ontbreekt")
	}

	currency := strings.ToUpper(strings.TrimSpace(input.Currency))
	if currency == "" {
		currency = "EUR"
	}
	body := map[string]any{
		"amount_inquired": map[string]string{
			"value":    centsString(input.AmountCents),
			"currency": currency,
		},
		"description":        trimMax(input.Description, 9000),
		"merchant_reference": trimMax(input.MerchantReference, 100),
		"allow_bunqme":       true,
	}
	if redirectURL := strings.TrimSpace(input.RedirectURL); redirectURL != "" {
		body["redirect_url"] = redirectURL
	}

	env, err := session.client.doWithRequestID(
		ctx,
		http.MethodPost,
		fmt.Sprintf("/user/%d/monetary-account/%d/request-inquiry", userID, input.MonetaryAccountID),
		session.token,
		body,
		true,
		stableClientRequestID(input.IdempotencyKey),
	)
	if err != nil {
		return nil, classifyPaymentRequestWriteError(err)
	}
	request, err := paymentRequestFromCreateEnvelope(env)
	if err != nil {
		return nil, err
	}
	if request.ID > 0 && (request.BunqMeShareURL == nil || request.AmountValue == "" || request.Status == "") {
		detailEnv, detailErr := session.client.do(
			ctx,
			http.MethodGet,
			fmt.Sprintf("/user/%d/monetary-account/%d/request-inquiry/%d", userID, input.MonetaryAccountID, request.ID),
			session.token,
			nil,
			false,
		)
		if detailErr == nil {
			if detail, detailOK := findPaymentRequest(detailEnv); detailOK {
				request = mergePaymentRequest(request, detail)
			}
		}
	}
	return request, nil
}

func classifyPaymentRequestWriteError(err error) error {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) && !httpErr.AmbiguousWrite() &&
		httpErr.StatusCode >= 400 && httpErr.StatusCode < 500 {
		return fmt.Errorf("request-inquiry: %w", err)
	}
	return &AmbiguousWriteError{Err: fmt.Errorf("request-inquiry: %w", err)}
}

func paymentRequestFromCreateEnvelope(env *envelope) (*PaymentRequest, error) {
	request, ok := findPaymentRequest(env)
	if !ok || request.ID <= 0 {
		return nil, &AmbiguousWriteError{Err: errors.New("request-inquiry write succeeded but bunq response contains no provider id")}
	}
	return request, nil
}

func GetPaymentRequest(ctx context.Context, cfg Config, userID, monetaryAccountID, requestID int) (*PaymentRequest, error) {
	if monetaryAccountID <= 0 {
		return nil, errors.New("BUNQ_MONETARY_ACCOUNT_ID ontbreekt")
	}
	if requestID <= 0 {
		return nil, errors.New("bunq request id ontbreekt")
	}
	session, err := authenticate(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if userID <= 0 {
		userID = session.userID
	}
	if userID <= 0 {
		return nil, errors.New("BUNQ_USER_ID ontbreekt")
	}
	env, err := session.client.do(
		ctx,
		http.MethodGet,
		fmt.Sprintf("/user/%d/monetary-account/%d/request-inquiry/%d", userID, monetaryAccountID, requestID),
		session.token,
		nil,
		false,
	)
	if err != nil {
		return nil, fmt.Errorf("request-inquiry ophalen: %w", err)
	}
	request, ok := findPaymentRequest(env)
	if !ok {
		return nil, errors.New("request-inquiry ontbreekt in bunq response")
	}
	return request, nil
}

func ListPaymentRequests(ctx context.Context, cfg Config, userID, monetaryAccountID int) ([]PaymentRequest, error) {
	if monetaryAccountID <= 0 {
		return nil, errors.New("BUNQ_MONETARY_ACCOUNT_ID ontbreekt")
	}
	session, err := authenticate(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if userID <= 0 {
		userID = session.userID
	}
	if userID <= 0 {
		return nil, errors.New("BUNQ_USER_ID ontbreekt")
	}
	requests, err := listPaymentRequestsWithSession(ctx, session,
		fmt.Sprintf("/user/%d/monetary-account/%d/request-inquiry?count=200", userID, monetaryAccountID))
	if err != nil {
		return nil, fmt.Errorf("list request-inquiry: %w", err)
	}
	return requests, nil
}

const maxBunqListPages = 100

func listPaymentRequestsWithSession(ctx context.Context, session *sessionContext, firstPath string) ([]PaymentRequest, error) {
	var out []PaymentRequest
	seenIDs := make(map[int]struct{})
	err := walkBunqPages(ctx, session, firstPath, func(env *envelope) {
		for _, request := range findPaymentRequests(env) {
			if _, duplicate := seenIDs[request.ID]; duplicate {
				continue
			}
			seenIDs[request.ID] = struct{}{}
			out = append(out, request)
		}
	})
	return out, err
}

func listAccountsWithSession(ctx context.Context, session *sessionContext, firstPath string) ([]Account, error) {
	var out []Account
	seenIDs := make(map[int]struct{})
	err := walkBunqPages(ctx, session, firstPath, func(env *envelope) {
		for _, account := range findAccounts(env) {
			if _, duplicate := seenIDs[account.ID]; duplicate {
				continue
			}
			seenIDs[account.ID] = struct{}{}
			out = append(out, account)
		}
	})
	return out, err
}

func walkBunqPages(ctx context.Context, session *sessionContext, firstPath string, consume func(*envelope)) error {
	if session == nil || session.client == nil {
		return errors.New("bunq session ontbreekt")
	}
	next := strings.TrimSpace(firstPath)
	seenPages := make(map[string]struct{})
	for page := 0; next != ""; page++ {
		if page >= maxBunqListPages {
			return fmt.Errorf("bunq pagination overschrijdt veilige limiet van %d pagina's", maxBunqListPages)
		}
		resolved, err := session.client.resolveRequestURL(next)
		if err != nil {
			return err
		}
		if _, duplicate := seenPages[resolved]; duplicate {
			return errors.New("bunq pagination bevat een herhaalde pagina-URL")
		}
		seenPages[resolved] = struct{}{}
		env, err := session.client.do(ctx, http.MethodGet, next, session.token, nil, false)
		if err != nil {
			return err
		}
		consume(env)
		next = strings.TrimSpace(env.Pagination.OlderURL)
	}
	return nil
}

func authenticate(ctx context.Context, cfg Config) (*sessionContext, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, errors.New("BUNQ_API_KEY ontbreekt")
	}
	env := normalizeEnvironment(cfg.Environment)
	cacheKey := bunqCacheKey(env, apiKey, cfg.DeviceDescription)
	now := time.Now()
	bunqSessionCache.Lock()
	if cached, ok := bunqSessionCache.items[cacheKey]; ok && cached.session != nil && now.Before(cached.expiresAt) {
		session := cached.session
		bunqSessionCache.Unlock()
		return session, nil
	}
	// Keep the lock for cache-miss onboarding. Session creation is rare and this
	// prevents concurrent requests from registering duplicate bunq devices and
	// sessions for the same configuration.
	defer bunqSessionCache.Unlock()

	privateKey, err := rsa.GenerateKey(crand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("RSA key maken mislukt: %w", err)
	}

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

	sessionContext := &sessionContext{client: client, token: sessionToken, userID: userID, userTyp: userType}
	bunqSessionCache.items[cacheKey] = sessionCacheEntry{
		session:   sessionContext,
		expiresAt: time.Now().Add(bunqSessionCacheTTL),
	}
	return sessionContext, nil
}

func bunqCacheKey(env, apiKey, deviceDescription string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(apiKey)))
	return strings.Join([]string{
		env,
		hex.EncodeToString(sum[:]),
		strings.TrimSpace(deviceDescription),
	}, ":")
}

func (c *Client) do(ctx context.Context, method, path, token string, body any, signed bool) (*envelope, error) {
	return c.doWithRequestID(ctx, method, path, token, body, signed, "")
}

func (c *Client) doWithRequestID(ctx context.Context, method, path, token string, body any, signed bool, clientRequestID string) (*envelope, error) {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}

	requestURL, err := c.resolveRequestURL(path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("X-Bunq-Language", "nl_NL")
	req.Header.Set("X-Bunq-Region", "nl_NL")
	req.Header.Set("X-Bunq-Geolocation", "0 0 0 0 NL")
	if clientRequestID == "" {
		clientRequestID = requestID()
	}
	req.Header.Set("X-Bunq-Client-Request-Id", clientRequestID)
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
		return nil, &HTTPError{StatusCode: resp.StatusCode, Message: bunqError(raw, resp.StatusCode)}
	}

	var out envelope
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("bunq response lezen mislukt: %w", err)
	}
	return &out, nil
}

func (c *Client) resolveRequestURL(path string) (string, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}
	raw := strings.TrimSpace(path)
	next, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("ongeldige bunq pagina-URL: %w", err)
	}
	basePath := strings.TrimRight(base.Path, "/")
	if next.IsAbs() {
		if !strings.EqualFold(next.Scheme, base.Scheme) || !strings.EqualFold(next.Host, base.Host) {
			return "", errors.New("bunq pagination wees naar een andere origin")
		}
		if basePath != "" && next.Path != basePath && !strings.HasPrefix(next.Path, basePath+"/") {
			return "", errors.New("bunq pagination wees buiten het API-basispad")
		}
		return next.String(), nil
	}
	if strings.HasPrefix(next.Path, basePath+"/") && basePath != "" {
		base.Path, base.RawQuery, base.Fragment = "", "", ""
		return base.ResolveReference(next).String(), nil
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	return strings.TrimRight(c.baseURL, "/") + raw, nil
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
	Response   []map[string]json.RawMessage `json:"Response"`
	Error      []bunqErrorObject            `json:"Error"`
	Pagination bunqPagination               `json:"Pagination"`
}

type bunqPagination struct {
	OlderURL  string `json:"older_url"`
	NewerURL  string `json:"newer_url"`
	FutureURL string `json:"future_url"`
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

type requestInquiry struct {
	ID                int           `json:"id"`
	Status            string        `json:"status"`
	Description       string        `json:"description"`
	MerchantReference *string       `json:"merchant_reference"`
	BunqMeShareURL    *string       `json:"bunqme_share_url"`
	AmountInquired    *amountObject `json:"amount_inquired"`
}

type amountObject struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
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

func findPaymentRequests(env *envelope) []PaymentRequest {
	requests := make([]PaymentRequest, 0)
	for _, item := range env.Response {
		raw, ok := item["RequestInquiry"]
		if !ok {
			continue
		}
		var inquiry requestInquiry
		if err := json.Unmarshal(raw, &inquiry); err != nil || inquiry.ID <= 0 {
			continue
		}
		result := PaymentRequest{
			ID:                inquiry.ID,
			Status:            inquiry.Status,
			Description:       inquiry.Description,
			MerchantReference: inquiry.MerchantReference,
			BunqMeShareURL:    inquiry.BunqMeShareURL,
		}
		if inquiry.AmountInquired != nil {
			result.AmountValue = inquiry.AmountInquired.Value
			result.Currency = inquiry.AmountInquired.Currency
		}
		requests = append(requests, result)
	}
	return requests
}

func findPaymentRequest(env *envelope) (*PaymentRequest, bool) {
	for _, item := range env.Response {
		raw, ok := item["RequestInquiry"]
		if !ok {
			continue
		}
		var inquiry requestInquiry
		if err := json.Unmarshal(raw, &inquiry); err != nil || inquiry.ID <= 0 {
			continue
		}
		result := &PaymentRequest{
			ID:                inquiry.ID,
			Status:            inquiry.Status,
			Description:       inquiry.Description,
			MerchantReference: inquiry.MerchantReference,
			BunqMeShareURL:    inquiry.BunqMeShareURL,
		}
		if inquiry.AmountInquired != nil {
			result.AmountValue = inquiry.AmountInquired.Value
			result.Currency = inquiry.AmountInquired.Currency
		}
		return result, true
	}
	for _, item := range env.Response {
		raw, ok := item["Id"]
		if !ok {
			continue
		}
		var id idObject
		if err := json.Unmarshal(raw, &id); err == nil && id.ID > 0 {
			return &PaymentRequest{ID: id.ID}, true
		}
	}
	return nil, false
}

func mergePaymentRequest(base, detail *PaymentRequest) *PaymentRequest {
	if base == nil {
		return detail
	}
	if detail == nil {
		return base
	}
	if base.ID <= 0 {
		base.ID = detail.ID
	}
	if base.Status == "" {
		base.Status = detail.Status
	}
	if base.AmountValue == "" {
		base.AmountValue = detail.AmountValue
	}
	if base.Currency == "" {
		base.Currency = detail.Currency
	}
	if base.Description == "" {
		base.Description = detail.Description
	}
	if base.MerchantReference == nil {
		base.MerchantReference = detail.MerchantReference
	}
	if base.BunqMeShareURL == nil {
		base.BunqMeShareURL = detail.BunqMeShareURL
	}
	return base
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

func centsString(cents int) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

func trimMax(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	return strings.TrimSpace(value[:max])
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

func stableClientRequestID(idempotencyKey string) string {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return requestID()
	}
	sum := sha256.Sum256([]byte(idempotencyKey))
	return "homeapp-idem-" + hex.EncodeToString(sum[:16])
}
func requestID() string {
	var random [8]byte
	if _, err := crand.Read(random[:]); err != nil {
		return fmt.Sprintf("homeapp-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("homeapp-%d-%s", time.Now().UnixNano(), hex.EncodeToString(random[:]))
}
