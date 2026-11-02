package models

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Transaction struct {
	ID          int64     `json:"id"`
	Type        string    `json:"type"` // "income" or "expense"
	Amount      float64   `json:"amount"`
	Category    string    `json:"category"`
	Description string    `json:"description"`
	Date        time.Time `json:"date"`
	Source      string    `json:"source"` // "web" or "telegram"
	CreatedAt   time.Time `json:"created_at"`
}

type Summary struct {
	TotalIncome  float64 `json:"total_income"`
	TotalExpense float64 `json:"total_expense"`
	NetBalance   float64 `json:"net_balance"`
	Count        int     `json:"count"`
}

type CategoryTotal struct {
	Category string  `json:"category"`
	Type     string  `json:"type"`
	Total    float64 `json:"total"`
}

type DB struct {
	*sql.DB
}

func InitDB(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS transactions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		type TEXT NOT NULL,
		amount REAL NOT NULL,
		category TEXT NOT NULL,
		description TEXT NOT NULL,
		date DATETIME NOT NULL,
		source TEXT NOT NULL DEFAULT 'web',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_transactions_date ON transactions(date);
	CREATE INDEX IF NOT EXISTS idx_transactions_type ON transactions(type);

	CREATE TABLE IF NOT EXISTS config (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("exec schema: %w", err)
	}

	return &DB{DB: db}, nil
}

func (db *DB) AddTransaction(t *Transaction) (int64, error) {
	if t.Date.IsZero() {
		t.Date = time.Now()
	}
	if t.Source == "" {
		t.Source = "web"
	}
	t.Type = strings.ToLower(t.Type)
	if t.Type != "income" && t.Type != "expense" {
		t.Type = "expense"
	}
	if t.Category == "" {
		if t.Type == "income" {
			t.Category = "Pemasukan Lain"
		} else {
			t.Category = "Lain-lain"
		}
	}

	query := `INSERT INTO transactions (type, amount, category, description, date, source, created_at)
			  VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`
	res, err := db.Exec(query, t.Type, t.Amount, t.Category, t.Description, t.Date.Format(time.RFC3339), t.Source)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	t.ID = id
	return id, nil
}

func (db *DB) GetTransactionByID(id int64) (*Transaction, error) {
	query := "SELECT id, type, amount, category, description, date, source, created_at FROM transactions WHERE id = ?"
	var t Transaction
	var dateVal, createdAtVal string
	err := db.QueryRow(query, id).Scan(&t.ID, &t.Type, &t.Amount, &t.Category, &t.Description, &dateVal, &t.Source, &createdAtVal)
	if err != nil {
		return nil, err
	}
	t.Date, _ = time.Parse(time.RFC3339, dateVal)
	if t.Date.IsZero() {
		t.Date, _ = time.Parse("2006-01-02 15:04:05", dateVal)
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAtVal)
	if t.CreatedAt.IsZero() {
		t.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtVal)
	}
	return &t, nil
}

func (db *DB) UpdateTransaction(t *Transaction) error {
	t.Type = strings.ToLower(t.Type)
	if t.Type != "income" && t.Type != "expense" {
		t.Type = "expense"
	}
	query := `UPDATE transactions 
			  SET type = ?, amount = ?, category = ?, description = ?, date = ?
			  WHERE id = ?`
	_, err := db.Exec(query, t.Type, t.Amount, t.Category, t.Description, t.Date.Format(time.RFC3339), t.ID)
	return err
}

func (db *DB) DeleteTransaction(id int64) error {
	_, err := db.Exec("DELETE FROM transactions WHERE id = ?", id)
	return err
}

func (db *DB) GetTransactions(month string, dateStr string, limit int, offset int) ([]Transaction, int, error) {
	countQuery := "SELECT COUNT(id) FROM transactions WHERE 1=1"
	var countArgs []interface{}
	if month != "" {
		countQuery += " AND strftime('%Y-%m', date) = ?"
		countArgs = append(countArgs, month)
	}
	if dateStr != "" {
		countQuery += " AND strftime('%Y-%m-%d', date) = ?"
		countArgs = append(countArgs, dateStr)
	}

	var totalCount int
	if err := db.QueryRow(countQuery, countArgs...).Scan(&totalCount); err != nil {
		return nil, 0, err
	}

	query := "SELECT id, type, amount, category, description, date, source, created_at FROM transactions WHERE 1=1"
	var args []interface{}

	if month != "" { // Format "YYYY-MM"
		query += " AND strftime('%Y-%m', date) = ?"
		args = append(args, month)
	}
	if dateStr != "" { // Format "YYYY-MM-DD"
		query += " AND strftime('%Y-%m-%d', date) = ?"
		args = append(args, dateStr)
	}

	query += " ORDER BY date DESC, id DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
		if offset > 0 {
			query += fmt.Sprintf(" OFFSET %d", offset)
		}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []Transaction
	for rows.Next() {
		var t Transaction
		var dateVal, createdAtVal string
		if err := rows.Scan(&t.ID, &t.Type, &t.Amount, &t.Category, &t.Description, &dateVal, &t.Source, &createdAtVal); err != nil {
			return nil, 0, err
		}
		t.Date, _ = time.Parse(time.RFC3339, dateVal)
		if t.Date.IsZero() {
			t.Date, _ = time.Parse("2006-01-02 15:04:05", dateVal)
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339, createdAtVal)
		if t.CreatedAt.IsZero() {
			t.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtVal)
		}
		list = append(list, t)
	}
	return list, totalCount, nil
}

func (db *DB) GetSummary(month string, dateStr string) (Summary, error) {
	query := `SELECT 
		COALESCE(SUM(CASE WHEN type = 'income' THEN amount ELSE 0 END), 0) AS total_income,
		COALESCE(SUM(CASE WHEN type = 'expense' THEN amount ELSE 0 END), 0) AS total_expense,
		COUNT(id) AS count
		FROM transactions WHERE 1=1`
	var args []interface{}

	if month != "" {
		query += " AND strftime('%Y-%m', date) = ?"
		args = append(args, month)
	}
	if dateStr != "" {
		query += " AND strftime('%Y-%m-%d', date) = ?"
		args = append(args, dateStr)
	}

	var s Summary
	err := db.QueryRow(query, args...).Scan(&s.TotalIncome, &s.TotalExpense, &s.Count)
	if err != nil {
		return s, err
	}
	s.NetBalance = s.TotalIncome - s.TotalExpense
	return s, nil
}

func (db *DB) GetCategoryBreakdown(month string) ([]CategoryTotal, error) {
	query := `SELECT category, type, SUM(amount) as total 
		FROM transactions WHERE 1=1`
	var args []interface{}
	if month != "" {
		query += " AND strftime('%Y-%m', date) = ?"
		args = append(args, month)
	}
	query += " GROUP BY category, type ORDER BY total DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []CategoryTotal
	for rows.Next() {
		var c CategoryTotal
		if err := rows.Scan(&c.Category, &c.Type, &c.Total); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, nil
}

func (db *DB) GetDailyTrend(month string) ([]map[string]interface{}, error) {
	query := `SELECT strftime('%Y-%m-%d', date) as day,
		COALESCE(SUM(CASE WHEN type = 'income' THEN amount ELSE 0 END), 0) as income,
		COALESCE(SUM(CASE WHEN type = 'expense' THEN amount ELSE 0 END), 0) as expense
		FROM transactions 
		WHERE strftime('%Y-%m', date) = ?
		GROUP BY day ORDER BY day ASC`
	rows, err := db.Query(query, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []map[string]interface{}
	for rows.Next() {
		var day string
		var inc, exp float64
		if err := rows.Scan(&day, &inc, &exp); err != nil {
			return nil, err
		}
		res = append(res, map[string]interface{}{
			"day":     day,
			"income":  inc,
			"expense": exp,
		})
	}
	return res, nil
}
