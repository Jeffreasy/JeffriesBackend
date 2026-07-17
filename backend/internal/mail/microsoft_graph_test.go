package mail

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/config"
)

func TestGraphAttachmentsAcceptsValidFileAttachment(t *testing.T) {
	attachments, err := graphAttachments([]Attachment{
		{
			Name:         "quickstart.pdf",
			ContentType:  "application/pdf",
			ContentBytes: base64.StdEncoding.EncodeToString([]byte("%PDF sample")),
		},
	})
	if err != nil {
		t.Fatalf("graphAttachments returned error: %v", err)
	}
	if len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(attachments))
	}
	if attachments[0]["@odata.type"] != "#microsoft.graph.fileAttachment" {
		t.Fatalf("unexpected attachment type: %v", attachments[0]["@odata.type"])
	}
}

func TestGraphAttachmentsRejectsInvalidBase64(t *testing.T) {
	_, err := graphAttachments([]Attachment{
		{Name: "bad.pdf", ContentType: "application/pdf", ContentBytes: "not-base64"},
	})
	if err == nil {
		t.Fatal("expected invalid base64 error")
	}
}

func TestGraphAttachmentsRejectsOversizedAttachment(t *testing.T) {
	content := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", maxAttachmentBytes+1)))
	_, err := graphAttachments([]Attachment{
		{Name: "too-large.pdf", ContentType: "application/pdf", ContentBytes: content},
	})
	if err == nil {
		t.Fatal("expected oversized attachment error")
	}
}

type graphRoundTripFunc func(*http.Request) (*http.Response, error)

func (f graphRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func configuredTestSender(transport http.RoundTripper) *Sender {
	return &Sender{
		cfg: &config.Config{
			LaventeCareMailEnabled: true, MicrosoftTenantID: "tenant",
			MicrosoftClientID: "client", MicrosoftClientSecret: "secret",
			MicrosoftSenderEmail: "sender@example.com",
		},
		http:  &http.Client{Transport: transport},
		token: &tokenCache{accessToken: "token", expiresAt: time.Now().Add(time.Hour)},
	}
}

func graphJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestSendDraftTimeoutThenImmediateDraftRemainsUnknown(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	sender := configuredTestSender(graphRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		switch calls {
		case 1:
			if req.Method != http.MethodPost || !strings.HasSuffix(req.URL.Path, "/send") {
				t.Fatalf("first request = %s %s", req.Method, req.URL.Path)
			}
			return nil, context.DeadlineExceeded
		case 2:
			return graphJSONResponse(http.StatusOK, `{"id":"draft-1","isDraft":true,"conversationId":"conv-1"}`), nil
		case 3:
			// Delayed Graph completion: the same immutable ID is now in Sent Items.
			return graphJSONResponse(http.StatusOK, `{"id":"draft-1","isDraft":false,"sentDateTime":"2026-07-17T10:00:01Z","conversationId":"conv-1"}`), nil
		default:
			t.Fatalf("unexpected Graph call %d", calls)
			return nil, errors.New("unexpected call")
		}
	}))

	draft := &SendResult{ProviderMessageID: "draft-1"}
	_, err := sender.SendDraft(context.Background(), draft)
	if !IsDeliveryUnknown(err) {
		t.Fatalf("timeout followed by draft must remain delivery-unknown, got %v", err)
	}
	state, err := sender.MessageState(context.Background(), draft.ProviderMessageID)
	if err != nil || !state.Sent {
		t.Fatalf("delayed reconciliation did not observe sent state: state=%+v err=%v", state, err)
	}
}

func TestSendDraftExplicitNonRetryableFourHundredAndDraftIsDefiniteFailure(t *testing.T) {
	calls := 0
	sender := configuredTestSender(graphRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return graphJSONResponse(http.StatusBadRequest, `{"error":{"code":"ErrorInvalidRecipients"}}`), nil
		}
		return graphJSONResponse(http.StatusOK, `{"id":"draft-2","isDraft":true}`), nil
	}))

	_, err := sender.SendDraft(context.Background(), &SendResult{ProviderMessageID: "draft-2"})
	if err == nil || IsDeliveryUnknown(err) {
		t.Fatalf("explicit non-retryable 400 plus confirmed draft should be definite failure, got %v", err)
	}
	var graphErr *GraphHTTPError
	if !errors.As(err, &graphErr) || graphErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected typed GraphHTTPError, got %T %v", err, err)
	}
}
