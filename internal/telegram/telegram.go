// Package telegram sends the service notices and the database backups to their Telegram chats, through the Bot
// API. The bot token is part of every request URL, so it is removed from any error before it can reach a log.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const apiBase = "https://api.telegram.org"

// Client sends messages and documents with a bot.
type Client struct {
	token         string
	noticesChatID int64
	backupsChatID int64
	servicePrefix string // Prepended to every notice, e.g. "TagoIO data handler: "
	http          *http.Client
	baseURL       string
	dryRun        bool // Log instead of sending (a test instance must not post to the production chats)
	log           *slog.Logger
}

// New creates a Telegram client. In dry-run mode the notices and backups are logged and not sent.
func New(token string, noticesChatID, backupsChatID int64, serviceName string, dryRun bool, log *slog.Logger) *Client {
	return &Client{
		token: token, noticesChatID: noticesChatID, backupsChatID: backupsChatID,
		servicePrefix: serviceName + ": ",
		http:          &http.Client{Timeout: 2 * time.Minute}, // Backups can take a while to upload
		baseURL:       apiBase,
		dryRun:        dryRun,
		log:           log,
	}
}

// Notify sends a notice to the notices chat.
func (c *Client) Notify(ctx context.Context, message string) error {
	if c.dryRun {
		c.log.Info("Telegram: dry run, notice not sent", "text", message)
		return nil
	}
	body, _ := json.Marshal(map[string]any{"chat_id": c.noticesChatID, "text": c.servicePrefix + message})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("sendMessage"), bytes.NewReader(body))
	if err != nil {
		return c.redact(err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.send(req)
}

// SendBackup uploads a file to the backups chat, with a caption.
func (c *Client) SendBackup(ctx context.Context, path, caption string) error {
	if c.dryRun {
		c.log.Info("Telegram: dry run, backup not uploaded", "file", path)
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// The multipart body is streamed: a backup is never fully held in memory.
	pr, pw := io.Pipe()
	form := multipart.NewWriter(pw)
	go func() {
		err := func() error {
			if err := form.WriteField("chat_id", strconv.FormatInt(c.backupsChatID, 10)); err != nil {
				return err
			}
			if err := form.WriteField("caption", c.servicePrefix+caption); err != nil {
				return err
			}
			part, err := form.CreateFormFile("document", filepath.Base(path))
			if err != nil {
				return err
			}
			if _, err := io.Copy(part, file); err != nil {
				return err
			}
			return form.Close()
		}()
		pw.CloseWithError(err)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("sendDocument"), pr)
	if err != nil {
		return c.redact(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	return c.send(req)
}

func (c *Client) endpoint(method string) string {
	return c.baseURL + "/bot" + c.token + "/" + method
}

// send performs a Bot API call and checks its {"ok": true} answer.
func (c *Client) send(req *http.Request) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return c.redact(err)
	}
	defer resp.Body.Close()

	var answer struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err := json.Unmarshal(raw, &answer); err != nil || !answer.OK {
		return fmt.Errorf("telegram: HTTP %d: %s", resp.StatusCode, c.redact(errors.New(answer.Description)))
	}
	return nil
}

// redact removes the bot token from an error message.
func (c *Client) redact(err error) error {
	if err == nil || c.token == "" || !strings.Contains(err.Error(), c.token) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), c.token, "REDACTED"))
}
