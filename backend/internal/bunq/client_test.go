package bunq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPWriteClassification(t *testing.T) {
	for _, status := range []int{408, 409, 423, 429, 500, 503} {
		err := classifyPaymentRequestWriteError(&HTTPError{StatusCode: status, Message: "test"})
		if !IsAmbiguousWrite(err) {
			t.Errorf("status %d must be ambiguous, got %T %v", status, err, err)
		}
	}
	for _, status := range []int{400, 401, 403, 404, 422} {
		err := classifyPaymentRequestWriteError(&HTTPError{StatusCode: status, Message: "test"})
		if IsAmbiguousWrite(err) {
			t.Errorf("status %d should be definite non-retryable failure", status)
		}
	}
	if err := classifyPaymentRequestWriteError(context.DeadlineExceeded); !IsAmbiguousWrite(err) {
		t.Fatalf("transport timeout must be ambiguous: %v", err)
	}
}

func TestPaymentRequestFromCreateEnvelopeMissingIDIsAmbiguous(t *testing.T) {
	_, err := paymentRequestFromCreateEnvelope(&envelope{})
	if !IsAmbiguousWrite(err) {
		t.Fatalf("2xx without provider id must be ambiguous, got %v", err)
	}
}

func TestListPaymentRequestsPaginatesAndDeduplicates(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/user/7/monetary-account/9/request-inquiry":
			fmt.Fprintf(w, `{"Response":[{"RequestInquiry":{"id":3,"merchant_reference":"INV-3"}}],"Pagination":{"older_url":%q}}`, server.URL+"/v1/page-2")
		case "/v1/page-2":
			_, _ = w.Write([]byte(`{"Response":[{"RequestInquiry":{"id":3}},{"RequestInquiry":{"id":2,"merchant_reference":"INV-2"}}],"Pagination":{"older_url":""}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	session := &sessionContext{client: &Client{baseURL: server.URL + "/v1", httpClient: server.Client()}, token: "token"}
	requests, err := listPaymentRequestsWithSession(context.Background(), session, "/user/7/monetary-account/9/request-inquiry?count=200")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || requests[0].ID != 3 || requests[1].ID != 2 {
		t.Fatalf("paginated requests = %+v", requests)
	}
}

func TestListAccountsPaginates(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/accounts" {
			fmt.Fprintf(w, `{"Response":[{"MonetaryAccountBank":{"id":1}}],"Pagination":{"older_url":%q}}`, server.URL+"/v1/accounts-2")
			return
		}
		if r.URL.Path == "/v1/accounts-2" {
			_, _ = w.Write([]byte(`{"Response":[{"MonetaryAccountBank":{"id":2}}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	session := &sessionContext{client: &Client{baseURL: server.URL + "/v1", httpClient: server.Client()}}
	accounts, err := listAccountsWithSession(context.Background(), session, "/accounts?count=200")
	if err != nil || len(accounts) != 2 {
		t.Fatalf("accounts=%+v err=%v", accounts, err)
	}
}

func TestBunqPaginationRejectsLoopAndForeignOrigin(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"Response":[],"Pagination":{"older_url":%q}}`, server.URL+r.URL.RequestURI())
	}))
	defer server.Close()
	client := &Client{baseURL: server.URL + "/v1", httpClient: server.Client()}
	session := &sessionContext{client: client}
	if _, err := listPaymentRequestsWithSession(context.Background(), session, "/loop"); err == nil || !strings.Contains(err.Error(), "herhaalde") {
		t.Fatalf("expected repeated-page guard, got %v", err)
	}
	if _, err := client.resolveRequestURL("https://evil.example/v1/page"); err == nil {
		t.Fatal("foreign pagination origin accepted")
	}
}

func TestEnvelopeJSONWithoutRequestDoesNotBecomeDefiniteRetry(t *testing.T) {
	raw, _ := json.Marshal(envelope{Response: []map[string]json.RawMessage{{"Id": json.RawMessage(`{"id":0}`)}}})
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	_, err := paymentRequestFromCreateEnvelope(&env)
	var unknown *AmbiguousWriteError
	if !errors.As(err, &unknown) {
		t.Fatalf("response-loss result = %T %v", err, err)
	}
}
