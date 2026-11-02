package exporter

import (
	"bytes"
	"caku/internal/models"
	"encoding/csv"
	"fmt"
	"time"
)

// ExportCSV mengekspor daftar transaksi ke format CSV standard (kompatibel Google Sheets / Excel)
func ExportCSV(txs []models.Transaction) ([]byte, error) {
	var buf bytes.Buffer
	// Tulis UTF-8 BOM agar Microsoft Excel langsung membaca karakter dan encoding dengan benar
	buf.Write([]byte{0xEF, 0xBB, 0xBF})

	writer := csv.NewWriter(&buf)

	// Header Google Sheet/Excel
	headers := []string{"ID", "Tanggal", "Tipe", "Kategori", "Deskripsi", "Nominal (Rp)", "Sumber", "Dibuat Pada"}
	if err := writer.Write(headers); err != nil {
		return nil, err
	}

	for _, t := range txs {
		tipe := "Pemasukan"
		if t.Type == "expense" {
			tipe = "Pengeluaran"
		}
		row := []string{
			fmt.Sprintf("%d", t.ID),
			t.Date.Format("2006-01-02 15:04:05"),
			tipe,
			t.Category,
			t.Description,
			fmt.Sprintf("%.0f", t.Amount),
			t.Source,
			t.CreatedAt.Format(time.RFC3339),
		}
		if err := writer.Write(row); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return buf.Bytes(), writer.Error()
}
