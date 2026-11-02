package gsheet

import (
	"bytes"
	"caku/internal/models"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// Syncer menangani pengiriman dan sinkronisasi mutasi transaksi langsung ke Google Sheets.
// Mendukung Google Apps Script Web App (paling mudah, tanpa perlu ribet GCP console),
// sehingga setiap pengetikan di Telegram/Web langsung muncul sebagai baris baru di spreadsheet pengguna.
type Syncer struct {
	webAppURL string
	sheetID   string
	client    *http.Client
}

type SheetRowPayload struct {
	Action      string  `json:"action"` // "append" or "delete"
	ID          int64   `json:"id"`
	Date        string  `json:"date"`
	Type        string  `json:"type"`
	Category    string  `json:"category"`
	Description string  `json:"description"`
	Amount      float64 `json:"amount"`
	Source      string  `json:"source"`
	CreatedAt   string  `json:"created_at"`
}

func NewSyncer(webAppURL string, sheetID string) *Syncer {
	if webAppURL == "" && sheetID == "" {
		return nil
	}
	return &Syncer{
		webAppURL: webAppURL,
		sheetID:   sheetID,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

// AppendTransaction mengirimkan transaksi baru secara asinkron ke Google Sheets
func (s *Syncer) AppendTransaction(t *models.Transaction) {
	if s == nil || s.webAppURL == "" {
		return
	}

	go func() {
		tipe := "Pemasukan"
		if t.Type == "expense" {
			tipe = "Pengeluaran"
		}

		payload := SheetRowPayload{
			Action:      "append",
			ID:          t.ID,
			Date:        t.Date.Format("2006-01-02 15:04:05"),
			Type:        tipe,
			Category:    t.Category,
			Description: t.Description,
			Amount:      t.Amount,
			Source:      t.Source,
			CreatedAt:   t.CreatedAt.Format(time.RFC3339),
		}

		body, err := json.Marshal(payload)
		if err != nil {
			log.Printf("[GSHEET] Marshal payload error: %v", err)
			return
		}

		req, err := http.NewRequest("POST", s.webAppURL, bytes.NewBuffer(body))
		if err != nil {
			log.Printf("[GSHEET] NewRequest error: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := s.client.Do(req)
		if err != nil {
			log.Printf("[GSHEET] Gagal sync ke Google Sheets: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			log.Printf("[GSHEET] Transaksi #%d berhasil di-sync ke Google Sheets", t.ID)
		} else {
			respBody, _ := io.ReadAll(resp.Body)
			log.Printf("[GSHEET] Respons gagal Google Sheets (%d): %s", resp.StatusCode, string(respBody))
		}
	}()
}

// UpdateTransaction mengirimkan perubahan data transaksi ke Google Sheets
func (s *Syncer) UpdateTransaction(t *models.Transaction) {
	if s == nil || s.webAppURL == "" {
		return
	}

	go func() {
		tipe := "Pemasukan"
		if t.Type == "expense" {
			tipe = "Pengeluaran"
		}

		payload := SheetRowPayload{
			Action:      "update",
			ID:          t.ID,
			Date:        t.Date.Format("2006-01-02 15:04:05"),
			Type:        tipe,
			Category:    t.Category,
			Description: t.Description,
			Amount:      t.Amount,
			Source:      t.Source,
		}

		body, err := json.Marshal(payload)
		if err != nil {
			return
		}

		req, err := http.NewRequest("POST", s.webAppURL, bytes.NewBuffer(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := s.client.Do(req)
		if err != nil {
			log.Printf("[GSHEET] Gagal sync update ke Google Sheets: %v", err)
			return
		}
		defer resp.Body.Close()
		log.Printf("[GSHEET] Update #%d disinkronkan ke Google Sheets", t.ID)
	}()
}

// SyncAllTransactions mengirimkan seluruh transaksi lokal (reconciliation) ke Google Sheets
func (s *Syncer) SyncAllTransactions(txs []models.Transaction) (int, error) {
	if s == nil || s.webAppURL == "" {
		return 0, fmt.Errorf("Google Sheets URL belum dikonfigurasi di .env")
	}

	type BatchPayload struct {
		Action string            `json:"action"` // "batch_sync"
		Rows   []SheetRowPayload `json:"rows"`
	}

	var rows []SheetRowPayload
	for _, t := range txs {
		tipe := "Pemasukan"
		if t.Type == "expense" {
			tipe = "Pengeluaran"
		}
		rows = append(rows, SheetRowPayload{
			ID:          t.ID,
			Date:        t.Date.Format("2006-01-02 15:04:05"),
			Type:        tipe,
			Category:    t.Category,
			Description: t.Description,
			Amount:      t.Amount,
			Source:      t.Source,
			CreatedAt:   t.CreatedAt.Format(time.RFC3339),
		})
	}

	payload := BatchPayload{
		Action: "batch_sync",
		Rows:   rows,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequest("POST", s.webAppURL, bytes.NewBuffer(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("koneksi ke Google Sheets gagal: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return len(rows), nil
	}
	respBody, _ := io.ReadAll(resp.Body)
	return 0, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
}

// DeleteTransaction mengirimkan sinyal penghapusan baris ke Google Sheets
func (s *Syncer) DeleteTransaction(id int64) {
	if s == nil || s.webAppURL == "" {
		return
	}

	go func() {
		payload := SheetRowPayload{
			Action: "delete",
			ID:     id,
		}

		body, _ := json.Marshal(payload)
		req, err := http.NewRequest("POST", s.webAppURL, bytes.NewBuffer(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := s.client.Do(req)
		if err != nil {
			log.Printf("[GSHEET] Gagal sync hapus ke Google Sheets: %v", err)
			return
		}
		resp.Body.Close()
		log.Printf("[GSHEET] Sinyal hapus #%d dikirim ke Google Sheets", id)
	}()
}

// GoogleAppsScriptTemplate menghasilkan kode Google Apps Script siap copy-paste
func GoogleAppsScriptTemplate() string {
	return `// ========================================================
// SCRIPT GOOGLE APPS SCRIPT UNTUK DATABASE CAKU
// ========================================================
// Cara Pasang:
// 1. Buka spreadsheet Google Sheets Anda.
// 2. Klik menu 'Extensions' -> 'Apps Script'.
// 3. Hapus kode lama, lalu PASTE kode di bawah ini.
// 4. Klik 'Deploy' -> 'New deployment'.
// 5. Pilih type 'Web app'.
// 6. Execute as: 'Me', Who has access: 'Anyone'.
// 7. Klik Deploy, copy 'Web App URL' dan masukkan ke file .env Caku:
//    GOOGLE_SHEET_WEBAPP_URL=https://script.google.com/macros/s/.../exec
// ========================================================

function doPost(e) {
  var lock = LockService.getScriptLock();
  lock.tryLock(10000);
  
  try {
    var sheet = SpreadsheetApp.getActiveSpreadsheet().getActiveSheet();
    var data = JSON.parse(e.postData.contents);
    
    // Buat header jika sheet masih kosong
    if (sheet.getLastRow() === 0) {
      sheet.appendRow([
        "ID", "Tanggal", "Tipe", "Kategori", "Deskripsi", "Nominal (Rp)", "Sumber", "Dibuat Pada"
      ]);
      sheet.getRange(1, 1, 1, 8).setFontWeight("bold").setBackground("#F1F5F9");
      sheet.setFrozenRows(1);
    }
    
    if (data.action === "append") {
      sheet.appendRow([
        data.id,
        data.date,
        data.type,
        data.category,
        data.description,
        data.amount,
        data.source,
        data.created_at
      ]);
      return ContentService.createTextOutput(JSON.stringify({result: "success", id: data.id}))
        .setMimeType(ContentService.MimeType.JSON);
    }
    
    if (data.action === "update") {
      var rows = sheet.getDataRange().getValues();
      for (var i = 1; i < rows.length; i++) {
        if (rows[i][0] == data.id) {
          // Update kolom: Tanggal(2), Tipe(3), Kategori(4), Deskripsi(5), Nominal(6)
          sheet.getRange(i + 1, 2, 1, 5).setValues([[
            data.date,
            data.type,
            data.category,
            data.description,
            data.amount
          ]]);
          break;
        }
      }
      return ContentService.createTextOutput(JSON.stringify({result: "updated", id: data.id}))
        .setMimeType(ContentService.MimeType.JSON);
    }

    if (data.action === "delete") {
      var rows = sheet.getDataRange().getValues();
      for (var i = 1; i < rows.length; i++) {
        if (rows[i][0] == data.id) {
          sheet.deleteRow(i + 1);
          break;
        }
      }
      return ContentService.createTextOutput(JSON.stringify({result: "deleted", id: data.id}))
        .setMimeType(ContentService.MimeType.JSON);
    }

    if (data.action === "batch_sync" && Array.isArray(data.rows)) {
      // Bersihkan data lama, buat ulang seluruh baris dari SQLite Caku
      sheet.clearContents();
      sheet.appendRow([
        "ID", "Tanggal", "Tipe", "Kategori", "Deskripsi", "Nominal (Rp)", "Sumber", "Dibuat Pada"
      ]);
      sheet.getRange(1, 1, 1, 8).setFontWeight("bold").setBackground("#F1F5F9");
      sheet.setFrozenRows(1);

      if (data.rows.length > 0) {
        var newRows = [];
        for (var k = 0; k < data.rows.length; k++) {
          var r = data.rows[k];
          newRows.push([
            r.id, r.date, r.type, r.category, r.description, r.amount, r.source, r.created_at
          ]);
        }
        sheet.getRange(2, 1, newRows.length, 8).setValues(newRows);
      }
      return ContentService.createTextOutput(JSON.stringify({result: "synced", count: data.rows.length}))
        .setMimeType(ContentService.MimeType.JSON);
    }
    
    return ContentService.createTextOutput(JSON.stringify({result: "ignored"}))
      .setMimeType(ContentService.MimeType.JSON);
      
  } catch (err) {
    return ContentService.createTextOutput(JSON.stringify({result: "error", error: err.toString()}))
      .setMimeType(ContentService.MimeType.JSON);
  } finally {
    lock.releaseLock();
  }
}

function doGet() {
  return ContentService.createTextOutput("Caku Google Sheets Database Active.");
}
`
}
