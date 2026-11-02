package telebot

import (
	"caku/internal/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRenderRichTableUsesNativeTable(t *testing.T) {
	db, err := models.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer db.Close()

	_, err = db.AddTransaction(&models.Transaction{
		Type: "expense", Amount: 25000,
		Category: "Makanan & Minuman", Description: "makan ayam",
		Date: time.Now(), Source: "telegram",
	})
	if err != nil {
		t.Fatalf("add transaction: %v", err)
	}

	var got struct {
		RichMessage struct {
			HTML string `json:"html"`
		} `json:"rich_message"`
		MessageID int64 `json:"message_id"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	b := &Bot{apiURL: srv.URL, db: db, httpClient: srv.Client()}

	b.sendRichTable(123, 1)
	if !strings.Contains(got.RichMessage.HTML, "<table bordered striped compact>") {
		t.Fatalf("expected native table, got: %s", got.RichMessage.HTML)
	}
	if strings.Contains(got.RichMessage.HTML, "<pre>") {
		t.Fatal("legacy ASCII table should not be used")
	}
	if !strings.Contains(got.RichMessage.HTML, "Makanan &amp; Minuman") {
		t.Fatal("category should be HTML-escaped and complete")
	}
	if !strings.Contains(got.RichMessage.HTML, "-Rp 25.000") {
		t.Fatal("nominal should use full rupiah format")
	}
	if !strings.Contains(got.RichMessage.HTML, "Total Keseluruhan") || !strings.Contains(got.RichMessage.HTML, "Saldo") {
		t.Fatal("summary totals should be shown under the table")
	}

	got.MessageID = 0
	b.renderRichTable(123, 42, 1, true)
	if got.MessageID != 42 {
		t.Fatalf("pagination should edit message 42, got %d", got.MessageID)
	}
}
