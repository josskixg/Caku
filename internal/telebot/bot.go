package telebot

import (
	"bytes"
	"caku/internal/exporter"
	"caku/internal/format"
	"caku/internal/gsheet"
	"caku/internal/models"
	"caku/internal/parser"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Bot struct {
	token      string
	apiURL     string
	db         *models.DB
	syncer     *gsheet.Syncer
	httpClient *http.Client
	stopChan   chan struct{}
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	From      User   `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
	Date      int64  `json:"date"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

func New(token string, db *models.DB, syncer *gsheet.Syncer) *Bot {
	return &Bot{
		token:      token,
		apiURL:     fmt.Sprintf("https://api.telegram.org/bot%s", token),
		db:         db,
		syncer:     syncer,
		httpClient: &http.Client{Timeout: 35 * time.Second},
		stopChan:   make(chan struct{}),
	}
}

func (b *Bot) Start() {
	if b.token == "" {
		log.Println("[TELEGRAM] Token kosong, bot telegram dinonaktifkan.")
		return
	}

	log.Println("[TELEGRAM] Memulai Long Polling Telegram Bot Caku...")
	offset := int64(0)

	for {
		select {
		case <-b.stopChan:
			log.Println("[TELEGRAM] Bot dihentikan.")
			return
		default:
		}

		updates, err := b.getUpdates(offset)
		if err != nil {
			log.Printf("[TELEGRAM] Gagal fetch updates: %s. Retry 3s...", strings.ReplaceAll(err.Error(), b.token, "***"))
			time.Sleep(3 * time.Second)
			continue
		}

		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}

			if u.Message != nil {
				go b.handleMessage(u.Message)
			} else if u.CallbackQuery != nil {
				go b.handleCallback(u.CallbackQuery)
			}
		}
	}
}

func (b *Bot) Stop() {
	close(b.stopChan)
}

func (b *Bot) getUpdates(offset int64) ([]Update, error) {
	endpoint := fmt.Sprintf("%s/getUpdates?offset=%d&timeout=25", b.apiURL, offset)
	resp, err := b.httpClient.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var res struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return res.Result, nil
}

func (b *Bot) handleMessage(msg *Message) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	chatID := msg.Chat.ID

	// Command handler
	if strings.HasPrefix(text, "/") {
		cmd := strings.Split(text, " ")[0]
		// potong bot username di command group misalnya /start@CakuBot
		if idx := strings.Index(cmd, "@"); idx != -1 {
			cmd = cmd[:idx]
		}

		switch cmd {
		case "/start", "/help":
			b.sendHelp(chatID)
			return
		case "/laporan", "/bulan", "/report":
			b.sendMonthlyReport(chatID, time.Now().Format("2006-01"))
			return
		case "/hari", "/today":
			b.sendDailyReport(chatID, time.Now().Format("2006-01-02"))
			return
		case "/tabel", "/table", "/riwayat":
			b.sendRichTable(chatID, 1)
			return
		case "/export", "/csv", "/excel":
			b.sendExportCSV(chatID)
			return
		case "/kategori":
			b.sendCategoryBreakdown(chatID, time.Now().Format("2006-01"))
			return
		default:
			// Lanjutkan mencoba parse jika user ngetik /+50k atau serupa
			text = strings.TrimPrefix(text, "/")
		}
	}

	// Parsing transaksi natural text (e.g. "+50k Gaji", "-25000 Makan warteg", "Beli bensin 20000")
	parsed, err := parser.ParseTransaction(text)
	if err != nil {
		// Pesan tidak dikenali sebagai transaksi, kirim hint interaktif
		b.SendMessage(chatID, "⚠️ <b>Format tidak dikenali</b>\n\nKetik langsung:\n• <code>-25000 Makan siang</code>\n• <code>+500k Bonus project</code>\n• <code>Beli bensin 20k</code>\n\nAtau ketik /help untuk menu lengkap.", nil)
		return
	}

	tx := &models.Transaction{
		Type:        parsed.Type,
		Amount:      parsed.Amount,
		Category:    parsed.Category,
		Description: parsed.Description,
		Date:        parsed.Date,
		Source:      "telegram",
	}

	id, err := b.db.AddTransaction(tx)
	if err != nil {
		b.SendMessage(chatID, "❌ Gagal menyimpan transaksi: "+format.EscapeHTML(err.Error()), nil)
		return
	}
	if b.syncer != nil {
		b.syncer.AppendTransaction(tx)
	}

	icon := "🔴"
	tipeStr := "Pengeluaran"
	if tx.Type == "income" {
		icon = "🟢"
		tipeStr = "Pemasukan"
	}

	response := fmt.Sprintf(
		"✅ <b>Transaksi Berhasil Dicatat!</b>\n\n"+
			"%s <b>%s:</b> <code>%s</code>\n"+
			"🏷️ <b>Kategori:</b> %s\n"+
			"📝 <b>Keterangan:</b> %s\n"+
			"📅 <b>Waktu:</b> %s\n"+
			"🆔 <b>ID:</b> #%d",
		icon, tipeStr, format.Rupiah(tx.Amount),
		format.EscapeHTML(tx.Category),
		format.EscapeHTML(tx.Description),
		format.DateID(tx.Date),
		id,
	)

	// Quick action inline buttons
	kbd := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "📊 Laporan Bulan Ini", CallbackData: "rep_month"},
				{Text: "📋 Tabel Riwayat", CallbackData: "show_table"},
			},
			{
				{Text: "🗑️ Hapus Transaksi Ini", CallbackData: fmt.Sprintf("del_%d", id)},
			},
		},
	}

	b.SendMessage(chatID, response, kbd)
}

func (b *Bot) handleCallback(cq *CallbackQuery) {
	chatID := cq.Message.Chat.ID
	msgID := cq.Message.MessageID
	data := cq.Data

	b.answerCallbackQuery(cq.ID)

	switch {
	case data == "rep_month":
		b.sendMonthlyReport(chatID, time.Now().Format("2006-01"))
	case data == "show_table":
		b.renderRichTable(chatID, msgID, 1, false)
	case strings.HasPrefix(data, "tbl_p_"):
		pageStr := strings.TrimPrefix(data, "tbl_p_")
		page, _ := strconv.Atoi(pageStr)
		if page <= 0 {
			page = 1
		}
		// Edit in-place pesan tabel sebelumnya agar mulus tanpa kirim pesan baru
		b.renderRichTable(chatID, msgID, page, true)
	case data == "noop":
		// Tombol info halaman (tidak melakukan aksi)
		return
	case data == "rep_today":
		b.sendDailyReport(chatID, time.Now().Format("2006-01-02"))
	case data == "export_csv":
		b.sendExportCSV(chatID)
	case strings.HasPrefix(data, "del_"):
		idStr := strings.TrimPrefix(data, "del_")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		if id > 0 {
			if err := b.db.DeleteTransaction(id); err == nil {
				if b.syncer != nil {
					b.syncer.DeleteTransaction(id)
				}
				delMsg := fmt.Sprintf("🗑️ Transaksi <b>#%d</b> telah berhasil dihapus.", id)
				_ = b.EditMessageText(chatID, msgID, delMsg, nil)
			} else {
				b.SendMessage(chatID, "❌ Gagal menghapus: "+err.Error(), nil)
			}
		}
	}
}

func (b *Bot) sendHelp(chatID int64) {
	helpText := `👛 <b>Selamat Datang di Caku (Catatan Keuangan)!</b>
Sistem pencatatan keuangan pintar terintegrasi Telegram, Web, dan Spreadsheet.

<b>🚀 Cara Cepat Mencatat:</b>
• <code>-25000 Makan bakso</code> (catat pengeluaran)
• <code>+1500000 Gaji bulanan</code> (catat pemasukan)
• <code>Beli bensin 25k</code> (otomatis pengeluaran)
• <code>Ngopi 15.000</code> (otomatis kategori Makanan)
• <code>Bonus 500k</code> (otomatis pemasukan)

<b>⚡ Perintah Lengkap:</b>
/hari - Ringkasan keuangan hari ini
/laporan - Laporan keuangan bulan berjalan
/tabel - Rich Table 10 transaksi terakhir
/kategori - Rincian pengeluaran per kategori
/export - Download rekap format CSV / Excel
/help - Panduan penggunaan ini`

	kbd := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "📅 Hari Ini", CallbackData: "rep_today"},
				{Text: "📊 Bulan Ini", CallbackData: "rep_month"},
			},
			{
				{Text: "📋 Tabel Transaksi", CallbackData: "show_table"},
				{Text: "📥 Export CSV/Excel", CallbackData: "export_csv"},
			},
		},
	}
	b.SendMessage(chatID, helpText, kbd)
}

func (b *Bot) sendMonthlyReport(chatID int64, month string) {
	summary, err := b.db.GetSummary(month, "")
	if err != nil {
		b.SendMessage(chatID, "Gagal mengambil data laporan: "+err.Error(), nil)
		return
	}

	monthTitle := format.MonthID(month)

	saldoIcon := "🟢"
	if summary.NetBalance < 0 {
		saldoIcon = "🔴"
	}

	report := fmt.Sprintf(
		"📊 <b>LAPORAN KEUANGAN: %s</b>\n"+
			"━━━━━━━━━━━━━━━━━━━━━━━━━\n\n"+
			"🟢 <b>Total Pemasukan :</b> <code>%s</code>\n"+
			"🔴 <b>Total Pengeluaran:</b> <code>%s</code>\n"+
			"━━━━━━━━━━━━━━━━━━━━━━━━━\n"+
			"%s <b>Sisa Saldo Netto  :</b> <code>%s</code>\n"+
			"📝 <b>Total Transaksi  :</b> %d transaksi\n\n",
		strings.ToUpper(monthTitle),
		format.Rupiah(summary.TotalIncome),
		format.Rupiah(summary.TotalExpense),
		saldoIcon, format.Rupiah(summary.NetBalance),
		summary.Count,
	)

	// Top kategori pengeluaran
	categories, err := b.db.GetCategoryBreakdown(month)
	if err == nil && len(categories) > 0 {
		report += "<b>Top Pengeluaran per Kategori:</b>\n"
		count := 0
		for _, cat := range categories {
			if cat.Type == "expense" {
				pct := (cat.Total / summary.TotalExpense) * 100
				if summary.TotalExpense == 0 {
					pct = 0
				}
				report += fmt.Sprintf("• %s: <code>%s</code> (%.0f%%)\n", format.EscapeHTML(cat.Category), format.Rupiah(cat.Total), pct)
				count++
				if count >= 5 {
					break
				}
			}
		}
	}

	kbd := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "📋 Lihat Tabel", CallbackData: "show_table"},
				{Text: "📥 Download CSV", CallbackData: "export_csv"},
			},
		},
	}
	b.SendMessage(chatID, report, kbd)
}

func (b *Bot) sendDailyReport(chatID int64, dateStr string) {
	summary, err := b.db.GetSummary("", dateStr)
	if err != nil {
		b.SendMessage(chatID, "Gagal mengambil data: "+err.Error(), nil)
		return
	}

	now := time.Now()
	report := fmt.Sprintf(
		"📅 <b>REKAP HARI INI</b>\n"+
			"<i>%s</i>\n"+
			"━━━━━━━━━━━━━━━━━━━━━━━━━\n"+
			"🟢 Pemasukan  : <code>%s</code>\n"+
			"🔴 Pengeluaran: <code>%s</code>\n"+
			"💵 Saldo Hari Ini: <code>%s</code>\n"+
			"🔢 Transaksi : %d item\n",
		format.DateID(now),
		format.Rupiah(summary.TotalIncome),
		format.Rupiah(summary.TotalExpense),
		format.Rupiah(summary.NetBalance),
		summary.Count,
	)

	b.SendMessage(chatID, report, nil)
}

func (b *Bot) sendCategoryBreakdown(chatID int64, month string) {
	categories, err := b.db.GetCategoryBreakdown(month)
	if err != nil {
		b.SendMessage(chatID, "Gagal membaca kategori: "+err.Error(), nil)
		return
	}

	summary, _ := b.db.GetSummary(month, "")

	var bld strings.Builder
	bld.WriteString(fmt.Sprintf("🏷️ <b>RINCIAN PENGELUARAN PER KATEGORI</b>\n<i>Periode %s</i>\n\n", format.MonthID(month)))

	hasExpense := false
	for _, c := range categories {
		if c.Type == "expense" {
			hasExpense = true
			bar := format.ProgressBar(c.Total, summary.TotalExpense, 8)
			bld.WriteString(fmt.Sprintf("<b>%s</b>\n<code>%s</code> %s\n%s\n\n",
				format.EscapeHTML(c.Category),
				bar,
				format.Rupiah(c.Total),
				strings.Repeat("·", 20),
			))
		}
	}
	if !hasExpense {
		bld.WriteString("Belum ada catatan pengeluaran pada bulan ini.")
	}

	b.SendMessage(chatID, bld.String(), nil)
}

// sendRichTable mengirim tabel native Telegram Rich Message.
// page 1-based, 10 baris per halaman, plus inline keyboard pagination.
func (b *Bot) sendRichTable(chatID int64, page int) {
	b.renderRichTable(chatID, 0, page, false)
}

func (b *Bot) renderRichTable(chatID int64, messageID int64, page int, isEdit bool) {
	const perPage = 10

	txs, total, err := b.db.GetTransactions("", "", perPage, (page-1)*perPage)
	if err != nil {
		b.SendMessage(chatID, "Gagal mengambil riwayat transaksi: "+format.EscapeHTML(err.Error()), nil)
		return
	}

	if total == 0 {
		b.SendMessage(chatID, "Belum ada transaksi tersimpan. Ketik <code>-25000 Makan</code> untuk memulai!", nil)
		return
	}

	if len(txs) == 0 { // page melewati akhir data
		b.SendMessage(chatID, "Halaman tidak ditemukan. Ketik /tabel untuk mulai dari halaman 1.", nil)
		return
	}

	totalPages := (total + perPage - 1) / perPage

	var bld strings.Builder
	bld.WriteString("<h3>Riwayat Transaksi</h3>")
	bld.WriteString(fmt.Sprintf("<p>Halaman <b>%d/%d</b> · <b>%d</b> transaksi</p>", page, totalPages, total))
	bld.WriteString(`<table bordered striped compact>`)
	bld.WriteString(`<tr><th>Tanggal</th><th>Kategori</th><th>Keterangan</th><th align="right">Nominal</th></tr>`)

	for _, t := range txs {
		sign := "-"
		if t.Type == "income" {
			sign = "+"
		}
		desc := t.Description
		if len([]rune(desc)) > 48 {
			desc = string([]rune(desc)[:47]) + "…"
		}
		bld.WriteString(fmt.Sprintf(
			`<tr><td>%s</td><td>%s</td><td>%s</td><td align="right"><b>%s%s</b></td></tr>`,
			t.Date.Format("02/01/06"),
			format.EscapeHTML(t.Category),
			format.EscapeHTML(desc),
			sign, format.Rupiah(t.Amount),
		))
	}
	bld.WriteString(`</table>`)

	bld.WriteString(`</table>`)

	if summary, err := b.db.GetSummary("", ""); err == nil {
		bld.WriteString("<h4>Total Keseluruhan</h4>")
		bld.WriteString(`<table bordered striped compact>`)
		bld.WriteString(`<tr><th>Keterangan</th><th align="right">Nominal</th></tr>`)
		bld.WriteString(fmt.Sprintf(`<tr><td>Pemasukan</td><td align="right"><b>%s</b></td></tr>`, format.Rupiah(summary.TotalIncome)))
		bld.WriteString(fmt.Sprintf(`<tr><td>Pengeluaran</td><td align="right"><b>%s</b></td></tr>`, format.Rupiah(summary.TotalExpense)))
		bld.WriteString(fmt.Sprintf(`<tr><td>Saldo</td><td align="right"><b>%s</b></td></tr>`, format.Rupiah(summary.NetBalance)))
		bld.WriteString(`</table>`)
	}

	bld.WriteString("<footer>Tanda (+) pemasukan, (-) pengeluaran.</footer>")

	bld.WriteString("<footer>Tanda (+) pemasukan, (-) pengeluaran.</footer>")

	kbd := b.tableKeyboard(page, totalPages)
	if isEdit && messageID > 0 {
		if err := b.EditRichMessage(chatID, messageID, bld.String(), kbd); err != nil {
			log.Printf("[TELEGRAM] Gagal edit rich table: %v", err)
		}
	} else {
		if err := b.SendRichMessage(chatID, bld.String(), kbd); err != nil {
			log.Printf("[TELEGRAM] Gagal kirim rich table: %v", err)
		}
	}
}

// tableKeyboard menyusun navigasi pagination (dan lompat jauh bila halaman banyak).
func (b *Bot) tableKeyboard(page, totalPages int) *InlineKeyboardMarkup {
	var row []InlineKeyboardButton

	if page > 1 {
		row = append(row, InlineKeyboardButton{Text: "⬅️ Prev", CallbackData: fmt.Sprintf("tbl_p_%d", page-1)})
	}
	row = append(row, InlineKeyboardButton{Text: fmt.Sprintf("%d/%d", page, totalPages), CallbackData: "noop"})
	if page < totalPages {
		row = append(row, InlineKeyboardButton{Text: "Next ➡️", CallbackData: fmt.Sprintf("tbl_p_%d", page+1)})
	}

	kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{row}}

	// Lompatan cepat 10 halaman untuk dataset besar
	var jump []InlineKeyboardButton
	if page > 10 {
		jump = append(jump, InlineKeyboardButton{Text: "⏮️ -10", CallbackData: fmt.Sprintf("tbl_p_%d", page-10)})
	}
	if page+10 <= totalPages {
		jump = append(jump, InlineKeyboardButton{Text: "+10 ⏭️", CallbackData: fmt.Sprintf("tbl_p_%d", page+10)})
	}
	if len(jump) > 0 {
		kb.InlineKeyboard = append(kb.InlineKeyboard, jump)
	}

	kb.InlineKeyboard = append(kb.InlineKeyboard, []InlineKeyboardButton{
		{Text: "📊 Laporan Bulan Ini", CallbackData: "rep_month"},
		{Text: "📥 Export CSV", CallbackData: "export_csv"},
	})

	return kb
}

func (b *Bot) sendExportCSV(chatID int64) {
	txs, _, err := b.db.GetTransactions("", "", 0, 0)
	if err != nil {
		b.SendMessage(chatID, "Gagal mengekspor data: "+err.Error(), nil)
		return
	}

	if len(txs) == 0 {
		b.SendMessage(chatID, "Belum ada transaksi untuk diekspor.", nil)
		return
	}

	data, err := exporter.ExportCSV(txs)
	if err != nil {
		b.SendMessage(chatID, "Gagal mengolah file CSV: "+err.Error(), nil)
		return
	}

	filename := fmt.Sprintf("Caku_Laporan_%s.csv", time.Now().Format("20060102_1504"))
	caption := fmt.Sprintf("📊 <b>File Export Catatan Keuangan Caku</b>\nTotal: %d transaksi.\nFile siap dibuka di Excel / Google Sheets.", len(txs))

	if err := b.SendDocument(chatID, filename, data, caption); err != nil {
		b.SendMessage(chatID, "Gagal mengirim dokumen: "+err.Error(), nil)
	}
}

func (b *Bot) SendMessage(chatID int64, text string, replyMarkup *InlineKeyboardMarkup) error {
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := b.httpClient.Post(b.apiURL+"/sendMessage", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// SendRichMessage mengirim pesan dengan tabel/heading native Telegram Rich Message.
func (b *Bot) SendRichMessage(chatID int64, html string, replyMarkup *InlineKeyboardMarkup) error {
	return b.postRichMessage("sendRichMessage", chatID, 0, html, replyMarkup)
}

// EditRichMessage mengganti isi rich message di tempat (dipakai pagination tabel).
func (b *Bot) EditRichMessage(chatID int64, messageID int64, html string, replyMarkup *InlineKeyboardMarkup) error {
	return b.postRichMessage("editMessageText", chatID, messageID, html, replyMarkup)
}

func (b *Bot) postRichMessage(method string, chatID int64, messageID int64, html string, replyMarkup *InlineKeyboardMarkup) error {
	payload := map[string]interface{}{
		"chat_id":      chatID,
		"rich_message": map[string]string{"html": html},
	}
	if messageID > 0 {
		payload["message_id"] = messageID
	}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := b.httpClient.Post(b.apiURL+"/"+method, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s", method, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func (b *Bot) SendDocument(chatID int64, filename string, fileData []byte, caption string) error {
	var bBuf bytes.Buffer
	w := multipart.NewWriter(&bBuf)

	if err := w.WriteField("chat_id", strconv.FormatInt(chatID, 10)); err != nil {
		return err
	}
	if caption != "" {
		if err := w.WriteField("caption", caption); err != nil {
			return err
		}
		if err := w.WriteField("parse_mode", "HTML"); err != nil {
			return err
		}
	}

	part, err := w.CreateFormFile("document", filename)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, bytes.NewReader(fileData)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest("POST", b.apiURL+"/sendDocument", &bBuf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (b *Bot) EditMessageText(chatID int64, messageID int64, text string, replyMarkup *InlineKeyboardMarkup) error {
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := b.httpClient.Post(b.apiURL+"/editMessageText", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (b *Bot) answerCallbackQuery(id string) {
	values := url.Values{}
	values.Set("callback_query_id", id)
	_, _ = b.httpClient.PostForm(b.apiURL+"/answerCallbackQuery", values)
}
