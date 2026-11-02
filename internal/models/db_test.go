package models

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDBOperations(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_caku.db")

	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	// 1. Tambah Transaksi
	tx1 := &Transaction{
		Type:        "expense",
		Amount:      35000,
		Category:    "Makanan & Minuman",
		Description: "Nasi Padang",
		Date:        time.Now(),
		Source:      "telegram",
	}
	id1, err := db.AddTransaction(tx1)
	if err != nil || id1 <= 0 {
		t.Fatalf("AddTransaction 1 failed: %v", err)
	}

	tx2 := &Transaction{
		Type:        "income",
		Amount:      500000,
		Category:    "Gaji & Upah",
		Description: "Side project",
		Date:        time.Now(),
		Source:      "web",
	}
	id2, err := db.AddTransaction(tx2)
	if err != nil || id2 <= 0 {
		t.Fatalf("AddTransaction 2 failed: %v", err)
	}

	// 2. Summary
	curMonth := time.Now().Format("2006-01")
	summary, err := db.GetSummary(curMonth, "")
	if err != nil {
		t.Fatalf("GetSummary failed: %v", err)
	}
	if summary.TotalIncome != 500000 {
		t.Errorf("TotalIncome want 500000, got %v", summary.TotalIncome)
	}
	if summary.TotalExpense != 35000 {
		t.Errorf("TotalExpense want 35000, got %v", summary.TotalExpense)
	}
	if summary.NetBalance != 465000 {
		t.Errorf("NetBalance want 465000, got %v", summary.NetBalance)
	}
	if summary.Count != 2 {
		t.Errorf("Count want 2, got %d", summary.Count)
	}

	// 3. Delete
	if err := db.DeleteTransaction(id1); err != nil {
		t.Fatalf("DeleteTransaction failed: %v", err)
	}
	list, _, err := db.GetTransactions(curMonth, "", 0, 0)
	if err != nil {
		t.Fatalf("GetTransactions failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("Expected 1 transaction left, got %d", len(list))
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
