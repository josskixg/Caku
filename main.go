package main

import (
	"caku/internal/gsheet"
	"caku/internal/models"
	"caku/internal/telebot"
	"caku/internal/web"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func loadEnvFile() {
	// Membaca file .env sederhana jika ada (tanpa dependency eksternal)
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	lines := splitLines(string(data))
	for _, line := range lines {
		line = trimSpace(line)
		if line == "" || hasPrefix(line, "#") {
			continue
		}
		if idx := indexOf(line, "="); idx != -1 {
			key := trimSpace(line[:idx])
			val := trimSpace(line[idx+1:])
			val = trim(val, `"'`)
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
	}
}

func main() {
	loadEnvFile()

	dbPath := getEnv("CAKU_DB_PATH", "data/caku.db")
	tgToken := getEnv("TELEGRAM_BOT_TOKEN", "")
	webUser := getEnv("CAKU_WEB_USER", "admin")
	webPass := getEnv("CAKU_WEB_PASS", "caku123")
	port := getEnv("CAKU_PORT", "8080")
	sheetURL := getEnv("GOOGLE_SHEET_WEBAPP_URL", "")

	db, err := models.InitDB(dbPath)
	if err != nil {
		log.Fatalf("Gagal inisialisasi database: %v", err)
	}
	defer db.Close()
	log.Printf("✅ Database siap di %s", dbPath)

	// Google Sheets Syncer
	var syncer *gsheet.Syncer
	if sheetURL != "" {
		syncer = gsheet.NewSyncer(sheetURL, "")
		log.Println("🟢 Google Sheets Database aktif: setiap transaksi langsung tersinkron.")
	} else {
		log.Println("ℹ️  GOOGLE_SHEET_WEBAPP_URL kosong (transaksi tersimpan di SQLite lokal).")
	}

	// Start Telegram Bot
	var bot *telebot.Bot
	if tgToken != "" {
		bot = telebot.New(tgToken, db, syncer)
		go bot.Start()
	} else {
		log.Println("⚠️  TELEGRAM_BOT_TOKEN tidak di-set. Bot Telegram tidak berjalan.")
	}

	// Start Web Server
	server := web.NewServer(db, webUser, webPass, syncer)
	httpServer := &http.Server{
		Addr:         ":" + port,
		Handler:      server.Routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		log.Printf("🌐 Web Dashboard Caku berjalan di http://localhost:%s", port)
		log.Printf("🔐 Login: user=%s (ganti lewat file .env)", webUser)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Web server error: %v", err)
		}
	}()

	fmt.Print(`
   ______           __        
  / ____/___ ______/ /____  __
 / /   / __ ` + "`" + `/ ___/ //_/ / / / /
/ /___/ /_/ / /  / ,< / /_/ / 
\____/\__,_/_/  /_/|_|\__,_/  
                               
 Catatan Keuangan Cerdas - Telegram + Web + Spreadsheet
`)

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("Mematikan Caku...")
	if bot != nil {
		bot.Stop()
	}
	httpServer.Close()
	log.Println("Sampai jumpa! 👋")
}

// Helper string sederhana (menghindari import tambahan)
func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			out = append(out, line)
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func trimSpace(s string) string { return trim(s, " \t\r\n") }

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func trim(s, cutset string) string {
	isCut := func(r byte) bool {
		for i := 0; i < len(cutset); i++ {
			if cutset[i] == r {
				return true
			}
		}
		return false
	}
	start := 0
	for start < len(s) && isCut(s[start]) {
		start++
	}
	end := len(s)
	for end > start && isCut(s[end-1]) {
		end--
	}
	return s[start:end]
}
