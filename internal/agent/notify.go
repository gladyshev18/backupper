package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"backupper/internal/chunk"
	"backupper/internal/model"
)

// SendTelegram posts a message through the Bot API.
func SendTelegram(ctx context.Context, t model.Telegram, text string) error {
	if t.BotToken == "" || t.ChatID == "" {
		return fmt.Errorf("не заполнены токен бота или chat id")
	}
	form := url.Values{"chat_id": {t.ChatID}, "text": {text}, "parse_mode": {"HTML"}, "disable_web_page_preview": {"true"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+t.BotToken+"/sendMessage", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var res struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if !res.OK {
		return fmt.Errorf("Telegram: %s", res.Description)
	}
	return nil
}

func (a *Agent) notify(job model.Job, run *model.Run) {
	cfg := a.state.Config()
	t := cfg.Settings.Telegram
	if !t.Enabled {
		return
	}
	mode := job.Notify
	if mode == "" {
		mode = "errors"
	}
	failed := run.Status == model.RunFailed
	if mode == "never" || (mode == "errors" && !failed && run.Status != model.RunWarning) {
		return
	}
	icon := map[string]string{model.RunSuccess: "✅", model.RunWarning: "⚠️", model.RunFailed: "❌", model.RunCanceled: "⏹"}[run.Status]
	title := map[string]string{model.RunSuccess: "Бэкап выполнен", model.RunWarning: "Бэкап с предупреждениями", model.RunFailed: "Бэкап не выполнен", model.RunCanceled: "Бэкап отменён"}[run.Status]
	var b strings.Builder
	fmt.Fprintf(&b, "%s <b>%s</b>\n", icon, title)
	fmt.Fprintf(&b, "Сервер: <b>%s</b>\nЗадача: %s\n", html.EscapeString(a.serverName()), html.EscapeString(job.Name))
	fmt.Fprintf(&b, "Длительность: %s, объём: %s\n", ruDuration(run.FinishedAt.Sub(run.StartedAt)), chunk.HumanBytes(run.Bytes))
	if run.Error != "" {
		e := run.Error
		if len(e) > 1500 {
			e = e[:1500] + "…"
		}
		fmt.Fprintf(&b, "\n<code>%s</code>", html.EscapeString(e))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := SendTelegram(ctx, t, b.String()); err != nil {
		a.logf(run, "warn", "Уведомление в Telegram не отправлено: %v", err)
		a.state.SaveHistory()
	}
}
