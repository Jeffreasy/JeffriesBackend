package mail

import (
	"encoding/base64"
	"strings"
	"testing"
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
