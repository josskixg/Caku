package format

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Rp format: Rp 1.250.000
func Rupiah(v float64) string {
	neg := v < 0
	v = math.Abs(v)
	s := strconv.FormatFloat(math.Round(v), 'f', 0, 64)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	out := "Rp " + b.String()
	if neg {
		return "-" + out
	}
	return out
}

func DateID(t time.Time) string {
	hari := [...]string{"Minggu", "Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu"}
	bulan := [...]string{"", "Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"}
	return fmt.Sprintf("%s, %d %s %d", hari[t.Weekday()], t.Day(), bulan[int(t.Month())], t.Year())
}

func MonthID(month string) string {
	bulan := [...]string{"", "Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"}
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return month
	}
	return fmt.Sprintf("%s %d", bulan[int(t.Month())], t.Year())
}

// EscapeHTML untuk parse_mode HTML Telegram
func EscapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

// ProgressBar bar visual untuk perbandingan budget
func ProgressBar(used, total float64, width int) string {
	if total <= 0 {
		return strings.Repeat("░", width)
	}
	ratio := used / total
	if ratio > 1 {
		ratio = 1
	}
	filled := int(math.Round(ratio * float64(width)))
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}
