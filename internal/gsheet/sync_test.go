package gsheet

import (
	"caku/internal/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSyncAllTransactionsPayload(t *testing.T) {
	var got struct {
		Action string            `json:"action"`
		Rows   []SheetRowPayload `json:"rows"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	syncer := NewSyncer(srv.URL, "")
	txs := []models.Transaction{
		{ID: 1, Type: "expense", Amount: 25000, Category: "Makanan & Minuman", Description: "siang", Date: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)},
		{ID: 2, Type: "income", Amount: 100000, Category: "Bonus", Description: "proyek", Date: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
	}

	count, err := syncer.SyncAllTransactions(txs)
	if err != nil {
		t.Fatalf("SyncAllTransactions: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	if got.Action != "batch_sync" || len(got.Rows) != 2 {
		t.Fatalf("payload = %+v", got)
	}
	if got.Rows[0].Type != "Pengeluaran" || got.Rows[1].Type != "Pemasukan" {
		t.Fatalf("type mapping = %q, %q", got.Rows[0].Type, got.Rows[1].Type)
	}
}

func TestSyncAllTransactionsRequiresURL(t *testing.T) {
	if _, err := NewSyncer("", "").SyncAllTransactions(nil); err == nil {
		t.Fatal("expected error when URL is empty")
	}
}

func TestAppsScriptTemplateHandlesCRUD(t *testing.T) {
	script := GoogleAppsScriptTemplate()
	for _, action := range []string{`"append"`, `"update"`, `"delete"`, `"batch_sync"`} {
		if !strings.Contains(script, action) {
			t.Errorf("template missing action %s", action)
		}
	}
}
