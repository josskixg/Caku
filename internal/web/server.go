package web

import (
	"caku/internal/exporter"
	"caku/internal/gsheet"
	"caku/internal/models"
	"caku/internal/parser"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	db           *models.DB
	syncer       *gsheet.Syncer
	username     string
	password     string
	sessionStore map[string]time.Time
	mu           sync.RWMutex
}

func NewServer(db *models.DB, user, pass string, syncer *gsheet.Syncer) *Server {
	if user == "" {
		user = "admin"
	}
	if pass == "" {
		pass = "caku123"
	}
	return &Server{
		db:           db,
		syncer:       syncer,
		username:     user,
		password:     pass,
		sessionStore: make(map[string]time.Time),
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Static assets
	fs := http.FileServer(http.Dir("web/static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// Auth routes
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)

	// Protected routes
	mux.HandleFunc("/", s.authMiddleware(s.handleDashboard))
	mux.HandleFunc("/api/transactions", s.authMiddleware(s.handleAPITransactions))
	mux.HandleFunc("/api/transactions/get", s.authMiddleware(s.handleAPIGetTransaction))
	mux.HandleFunc("/api/transactions/update", s.authMiddleware(s.handleAPIUpdateTransaction))
	mux.HandleFunc("/api/transactions/delete", s.authMiddleware(s.handleAPIDeleteTransaction))
	mux.HandleFunc("/api/stats", s.authMiddleware(s.handleAPIStats))
	mux.HandleFunc("/api/gsheet-script", s.authMiddleware(s.handleGSheetScript))
	mux.HandleFunc("/api/gsheet-sync-all", s.authMiddleware(s.handleAPIGSheetSyncAll))
	mux.HandleFunc("/export/csv", s.authMiddleware(s.handleExportCSV))

	return mux
}

// Authentication Middleware
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("caku_session")
		if err != nil || !s.isValidSession(cookie.Value) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (s *Server) isValidSession(token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	expiry, exists := s.sessionStore[token]
	if !exists {
		return false
	}
	return time.Now().Before(expiry)
}

func (s *Server) createSession() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	token := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessionStore[token] = time.Now().Add(24 * 7 * time.Hour) // 7 days session
	s.mu.Unlock()
	return token
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Jika sudah login, langsung ke dashboard
		if cookie, err := r.Cookie("caku_session"); err == nil && s.isValidSession(cookie.Value) {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		renderTemplate(w, "web/templates/login.html", map[string]interface{}{})
		return
	}

	if r.Method == http.MethodPost {
		username := r.FormValue("username")
		password := r.FormValue("password")

		uMatch := subtle.ConstantTimeCompare([]byte(username), []byte(s.username)) == 1
		pMatch := subtle.ConstantTimeCompare([]byte(password), []byte(s.password)) == 1

		if !uMatch || !pMatch {
			renderTemplate(w, "web/templates/login.html", map[string]interface{}{
				"Error": "Username atau Password salah! Periksa kembali.",
			})
			return
		}

		token := s.createSession()
		http.SetCookie(w, &http.Cookie{
			Name:     "caku_session",
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			Expires:  time.Now().Add(24 * 7 * time.Hour),
			SameSite: http.SameSiteLaxMode,
		})

		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("caku_session"); err == nil {
		s.mu.Lock()
		delete(s.sessionStore, cookie.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "caku_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	month := r.URL.Query().Get("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}

	pageStr := r.URL.Query().Get("page")
	page, _ := strconv.Atoi(pageStr)
	if page <= 0 {
		page = 1
	}
	pageSize := 15
	offset := (page - 1) * pageSize

	summary, err := s.db.GetSummary(month, "")
	if err != nil {
		log.Printf("error getting summary: %v", err)
	}

	transactions, totalCount, err := s.db.GetTransactions(month, "", pageSize, offset)
	if err != nil {
		log.Printf("error getting transactions: %v", err)
	}

	totalPages := (totalCount + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}

	data := map[string]interface{}{
		"Month":          month,
		"Summary":        summary,
		"Transactions":   transactions,
		"Username":       s.username,
		"IsGSheetActive": s.syncer != nil,
		"Page":           page,
		"TotalPages":     totalPages,
		"TotalCount":     totalCount,
		"HasPrev":        page > 1,
		"HasNext":        page < totalPages,
		"PrevPage":       page - 1,
		"NextPage":       page + 1,
	}

	renderTemplate(w, "web/templates/dashboard.html", data)
}

func (s *Server) handleAPITransactions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		month := r.URL.Query().Get("month")
		if month == "" {
			month = time.Now().Format("2006-01")
		}
		pageStr := r.URL.Query().Get("page")
		limitStr := r.URL.Query().Get("limit")
		page, _ := strconv.Atoi(pageStr)
		limit, _ := strconv.Atoi(limitStr)
		if limit <= 0 {
			limit = 50
		}
		if page <= 0 {
			page = 1
		}
		offset := (page - 1) * limit

		txs, total, err := s.db.GetTransactions(month, "", limit, offset)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data":  txs,
			"total": total,
			"page":  page,
			"limit": limit,
		})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Text        string  `json:"text"`
			Type        string  `json:"type"`
			Amount      float64 `json:"amount"`
			Category    string  `json:"category"`
			Description string  `json:"description"`
			Date        string  `json:"date"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}

		var tx models.Transaction

		// Jika input menggunakan teks cepat (Natural Language)
		if req.Text != "" {
			parsed, err := parser.ParseTransaction(req.Text)
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
				return
			}
			tx.Type = parsed.Type
			tx.Amount = parsed.Amount
			tx.Category = parsed.Category
			tx.Description = parsed.Description
			tx.Date = parsed.Date
		} else {
			tx.Type = req.Type
			tx.Amount = req.Amount
			tx.Category = req.Category
			tx.Description = req.Description
			if req.Date != "" {
				t, err := time.Parse("2006-01-02", req.Date)
				if err == nil {
					tx.Date = t
				}
			}
		}

		tx.Source = "web"
		id, err := s.db.AddTransaction(&tx)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		tx.ID = id
		if s.syncer != nil {
			s.syncer.AppendTransaction(&tx)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data":    tx,
		})
	}
}

func (s *Server) handleAPIGetTransaction(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}

	tx, err := s.db.GetTransactionByID(id)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tx)
}

func (s *Server) handleAPIUpdateTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID          int64   `json:"id"`
		Type        string  `json:"type"`
		Amount      float64 `json:"amount"`
		Category    string  `json:"category"`
		Description string  `json:"description"`
		Date        string  `json:"date"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	if req.ID <= 0 {
		http.Error(w, `{"error":"id required"}`, http.StatusBadRequest)
		return
	}

	tx, err := s.db.GetTransactionByID(req.ID)
	if err != nil {
		http.Error(w, `{"error":"transaksi tidak ditemukan"}`, http.StatusNotFound)
		return
	}

	tx.Type = req.Type
	tx.Amount = req.Amount
	tx.Category = req.Category
	tx.Description = req.Description
	if req.Date != "" {
		if t, err := time.Parse("2006-01-02", req.Date); err == nil {
			tx.Date = t
		}
	}

	if err := s.db.UpdateTransaction(tx); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	if s.syncer != nil {
		s.syncer.UpdateTransaction(tx)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    tx,
	})
}

func (s *Server) handleAPIGSheetSyncAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if s.syncer == nil {
		http.Error(w, `{"error":"GOOGLE_SHEET_WEBAPP_URL belum diatur di .env"}`, http.StatusBadRequest)
		return
	}

	// Ambil semua transaksi lokal
	txs, _, err := s.db.GetTransactions("", "", 0, 0)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	count, err := s.syncer.SyncAllTransactions(txs)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"synced":  count,
	})
}

func (s *Server) handleAPIDeleteTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := s.db.DeleteTransaction(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.syncer != nil {
		s.syncer.DeleteTransaction(id)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"success":true}`))
}

func (s *Server) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	month := r.URL.Query().Get("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}

	summary, err := s.db.GetSummary(month, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	categories, err := s.db.GetCategoryBreakdown(month)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	daily, err := s.db.GetDailyTrend(month)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"summary":    summary,
		"categories": categories,
		"daily":      daily,
	})
}

func (s *Server) handleGSheetScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(gsheet.GoogleAppsScriptTemplate()))
}

func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	month := r.URL.Query().Get("month")
	txs, _, err := s.db.GetTransactions(month, "", 0, 0)
	if err != nil {
		http.Error(w, "Gagal mengambil data: "+err.Error(), http.StatusInternalServerError)
		return
	}

	csvData, err := exporter.ExportCSV(txs)
	if err != nil {
		http.Error(w, "Gagal membuat CSV: "+err.Error(), http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("Caku_Laporan_%s.csv", time.Now().Format("20060102_150405"))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Write(csvData)
}

func renderTemplate(w http.ResponseWriter, tplPath string, data interface{}) {
	tmpl, err := template.New(filepath.Base(tplPath)).Funcs(template.FuncMap{
		"rupiah": func(v float64) string {
			return fmt.Sprintf("Rp %s", formatNumber(v))
		},
		"formatDate": func(t time.Time) string {
			return t.Format("02 Jan 2006 15:04")
		},
	}).ParseFiles(tplPath)

	if err != nil {
		log.Printf("template parse error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("template execute error: %v", err)
	}
}

func formatNumber(v float64) string {
	s := strconv.FormatFloat(v, 'f', 0, 64)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	return b.String()
}
