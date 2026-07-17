package handler

import (
	"encoding/base64"
	"testing"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

func TestValidateMailAttachmentsEnforcesPDFMIMEExtensionAndMagic(t *testing.T) {
	pdf := base64.StdEncoding.EncodeToString([]byte("%PDF-1.7\nexample"))
	valid := model.LCMailAttachment{Name: "document.pdf", ContentType: "application/pdf", ContentBytes: pdf}
	if err := validateMailAttachments([]model.LCMailAttachment{valid}); err != nil {
		t.Fatalf("valid PDF rejected: %v", err)
	}
	cases := []model.LCMailAttachment{
		{Name: "document.pdf", ContentType: "", ContentBytes: pdf},
		{Name: "document.txt", ContentType: "application/pdf", ContentBytes: pdf},
		{Name: "document.pdf", ContentType: "application/pdf", ContentBytes: base64.StdEncoding.EncodeToString([]byte("not a pdf"))},
	}
	for _, item := range cases {
		if err := validateMailAttachments([]model.LCMailAttachment{item}); err == nil {
			t.Fatalf("unsafe attachment accepted: %+v", item)
		}
	}
}
