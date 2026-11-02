package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ParsedEntry struct {
	Type        string // "income" or "expense"
	Amount      float64
	Category    string
	Description string
	Date        time.Time
}

// Format cepat via Telegram / Chat:
// "+50000 Gaji bulanan" -> income 50000
// "-25000 Makan siang warteg" -> expense 25000
// "Beli bensin 20000" / "Bensin 20k" / "Makan 15.000" -> expense
// "Pemasukan 1.5jt bonus project" -> income
// "Gaji +5000000" -> income
func ParseTransaction(text string) (*ParsedEntry, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("pesan kosong")
	}

	entry := &ParsedEntry{
		Date: time.Now(),
		Type: "expense", // default
	}

	// 1. Cek tanda awalan + atau -
	if strings.HasPrefix(text, "+") {
		entry.Type = "income"
		text = strings.TrimSpace(text[1:])
	} else if strings.HasPrefix(text, "-") {
		entry.Type = "expense"
		text = strings.TrimSpace(text[1:])
	}

	// 2. Deteksi keyword eksplisit jika belum ditandai
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "masuk ") || strings.HasPrefix(lower, "pemasukan ") || strings.HasPrefix(lower, "gaji ") || strings.HasPrefix(lower, "bonus ") || strings.HasPrefix(lower, "income ") {
		entry.Type = "income"
	} else if strings.HasPrefix(lower, "keluar ") || strings.HasPrefix(lower, "pengeluaran ") || strings.HasPrefix(lower, "beli ") || strings.HasPrefix(lower, "bayar ") || strings.HasPrefix(lower, "expense ") {
		entry.Type = "expense"
	}

	// 3. Regex ekstrak angka nominal uang (mendukung format: 50k, 100rb, 1.5jt, 50,000, 50000)
	numRegex := regexp.MustCompile(`(?i)(?:rp\.?\s*)?(\d+(?:[.,]\d+)?)\s*(k|rb|ribu|jt|juta|m|mio)?\b`)
	match := numRegex.FindStringSubmatchIndex(text)
	if match == nil {
		return nil, fmt.Errorf("tidak ditemukan nominal angka valid. Contoh: -25000 Makan atau +50k Gaji")
	}

	rawNum := text[match[2]:match[3]]
	unit := ""
	if match[4] != -1 && match[5] != -1 {
		unit = strings.ToLower(text[match[4]:match[5]])
	}

	// Normalisasi angka titik/koma
	cleanNum := rawNum
	if unit != "" {
		// Jika ada unit seperti 'jt' atau 'k', koma/titik adalah desimal (misal 1.5jt -> 1.5)
		cleanNum = strings.ReplaceAll(cleanNum, ",", ".")
	} else {
		// Jika nominal murni tanpa unit (misal 50.000 atau 1,000,000) titik/koma ribuan
		if strings.Contains(cleanNum, ".") && !strings.Contains(cleanNum, ",") {
			parts := strings.Split(cleanNum, ".")
			if len(parts) > 1 && len(parts[len(parts)-1]) == 3 {
				cleanNum = strings.ReplaceAll(cleanNum, ".", "")
			}
		} else if strings.Contains(cleanNum, ",") && !strings.Contains(cleanNum, ".") {
			parts := strings.Split(cleanNum, ",")
			if len(parts) > 1 && len(parts[len(parts)-1]) == 3 {
				cleanNum = strings.ReplaceAll(cleanNum, ",", "")
			} else {
				cleanNum = strings.ReplaceAll(cleanNum, ",", ".")
			}
		}
	}
	val, err := strconv.ParseFloat(cleanNum, 64)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca nominal: %w", err)
	}

	switch unit {
	case "k", "rb", "ribu":
		val *= 1000
	case "jt", "juta", "m", "mio":
		val *= 1000000
	}

	entry.Amount = val

	// 4. Ambil teks sisanya sebagai keterangan & kategori
	rest := strings.TrimSpace(text[:match[0]] + " " + text[match[1]:])
	// Hapus prefix keyword yang redundan
	for _, kw := range []string{"masuk", "pemasukan", "keluar", "pengeluaran", "beli", "bayar", "income", "expense"} {
		if strings.HasPrefix(strings.ToLower(rest), kw) {
			rest = strings.TrimSpace(rest[len(kw):])
		}
	}
	rest = strings.Trim(rest, "-+: ")
	if rest == "" {
		if entry.Type == "income" {
			rest = "Pemasukan Umum"
		} else {
			rest = "Pengeluaran Umum"
		}
	}

	entry.Description = rest
	entry.Category = AutoCategorize(entry.Type, rest)

	return entry, nil
}

// Auto categorizer cerdas berdasarkan kata kunci
func AutoCategorize(txType, desc string) string {
	d := strings.ToLower(desc)

	if txType == "income" {
		switch {
		case strings.Contains(d, "gaji") || strings.Contains(d, "salary"):
			return "Gaji & Upah"
		case strings.Contains(d, "bonus") || strings.Contains(d, "thr"):
			return "Bonus & THR"
		case strings.Contains(d, "invest") || strings.Contains(d, "dividen") || strings.Contains(d, "profit") || strings.Contains(d, "crypto"):
			return "Investasi & Bisnis"
		case strings.Contains(d, "hadiah") || strings.Contains(d, "gift"):
			return "Hadiah"
		default:
			return "Pemasukan Lain"
		}
	}

	// Expense category
	switch {
	case strings.Contains(d, "makan") || strings.Contains(d, "kopi") || strings.Contains(d, "cafe") || strings.Contains(d, "resto") || strings.Contains(d, "warteg") || strings.Contains(d, "snack") || strings.Contains(d, "minum"):
		return "Makanan & Minuman"
	case strings.Contains(d, "bensin") || strings.Contains(d, "ojol") || strings.Contains(d, "grab") || strings.Contains(d, "gojek") || strings.Contains(d, "parkir") || strings.Contains(d, "tol") || strings.Contains(d, "transport"):
		return "Transportasi"
	case strings.Contains(d, "listrik") || strings.Contains(d, "air") || strings.Contains(d, "wifi") || strings.Contains(d, "pulsa") || strings.Contains(d, "paket data") || strings.Contains(d, "sewa") || strings.Contains(d, "kost"):
		return "Tagihan & Utilitas"
	case strings.Contains(d, "belanja") || strings.Contains(d, "baju") || strings.Contains(d, "shopee") || strings.Contains(d, "tokped") || strings.Contains(d, "lazada"):
		return "Belanja & Kebutuhan"
	case strings.Contains(d, "obat") || strings.Contains(d, "dokter") || strings.Contains(d, "klinik") || strings.Contains(d, "sehat") || strings.Contains(d, "gym"):
		return "Kesehatan"
	case strings.Contains(d, "nonton") || strings.Contains(d, "game") || strings.Contains(d, "steam") || strings.Contains(d, "wisata") || strings.Contains(d, "hiburan"):
		return "Hiburan & Rekreasi"
	default:
		return "Lain-lain"
	}
}
