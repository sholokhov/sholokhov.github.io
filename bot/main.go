package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	stateIdle = iota
	stateWaitingLocation
	stateWaitingCamera
	stateWaitingCaption
)

const sessionFile = "session.json"

type session struct {
	State     int    `json:"state"`
	PhotoPath string `json:"photo_path"`
	Location  string `json:"location"`
	Camera    string `json:"camera"`
}

var sess session

var httpClient = &http.Client{Timeout: 30 * time.Second}

func send(bot *tgbotapi.BotAPI, chatID int64, text string) {
	bot.Send(tgbotapi.NewMessage(chatID, text))
}

func sendKeyboard(bot *tgbotapi.BotAPI, chatID int64, text string, kb tgbotapi.InlineKeyboardMarkup) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = kb
	bot.Send(msg)
}

func resetSession() {
	if sess.PhotoPath != "" {
		os.Remove(sess.PhotoPath)
	}
	os.Remove(sessionFile)
	sess = session{}
}

func saveSession() {
	data, err := json.Marshal(sess)
	if err != nil {
		log.Printf("Failed to marshal session: %v", err)
		return
	}
	if err := os.WriteFile(sessionFile, data, 0600); err != nil {
		log.Printf("Failed to write session file: %v", err)
	}
}

func restoreSession() bool {
	data, err := os.ReadFile(sessionFile)
	if err != nil {
		return false
	}
	if err := json.Unmarshal(data, &sess); err != nil {
		os.Remove(sessionFile)
		return false
	}
	if sess.PhotoPath != "" {
		if _, err := os.Stat(sess.PhotoPath); err != nil {
			os.Remove(sessionFile)
			sess = session{}
			return false
		}
	}
	return sess.State != stateIdle
}

func generateID() string {
	date := time.Now().Format("20060102")
	b := make([]byte, 3)
	rand.Read(b)
	return date + "-" + hex.EncodeToString(b)
}

func isPhotoMessage(msg *tgbotapi.Message) bool {
	return len(msg.Photo) > 0 || (msg.Document != nil && strings.HasPrefix(msg.Document.MimeType, "image/"))
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	store, err := NewStorage(cfg.B2)
	if err != nil {
		log.Fatalf("Failed to init storage: %v", err)
	}

	ghSync := NewGitHubSync(cfg.GitHub)

	bot, err := tgbotapi.NewBotAPI(cfg.Telegram.Token)
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	if restoreSession() {
		log.Printf("Restored in-progress session (state=%d)", sess.State)
	}
	log.Printf("Bot started as @%s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		var userID int64
		if update.Message != nil {
			userID = update.Message.From.ID
		} else if update.CallbackQuery != nil {
			userID = update.CallbackQuery.From.ID
		} else {
			continue
		}

		if userID != cfg.Telegram.AllowedUserID {
			if update.Message != nil {
				send(bot, update.Message.Chat.ID, "Sorry, this is a private bot.")
			}
			continue
		}

		if update.CallbackQuery != nil {
			handleCallback(bot, update.CallbackQuery, cfg)
			continue
		}

		msg := update.Message

		if msg.IsCommand() && msg.Command() == "cancel" {
			resetSession()
			send(bot, msg.Chat.ID, "Cancelled.")
			continue
		}
		if msg.IsCommand() && msg.Command() == "start" {
			send(bot, msg.Chat.ID, "Send me a photo to publish it to your gallery.")
			continue
		}

		if isPhotoMessage(msg) {
			if sess.State != stateIdle {
				send(bot, msg.Chat.ID, "Previous photo discarded. Starting over.")
				resetSession()
			}
			handlePhoto(bot, msg, cfg)
			continue
		}

		switch sess.State {
		case stateIdle:
			send(bot, msg.Chat.ID, "Send me a photo to get started.")
		case stateWaitingLocation:
			if sess.Location == "__awaiting_text" && msg.Text != "" {
				sess.Location = msg.Text
				sess.State = stateWaitingCamera
				saveSession()
				sendKeyboard(bot, msg.Chat.ID, "Select camera:", makeTagKeyboard("cam", cfg.Tags.Cameras))
			} else {
				send(bot, msg.Chat.ID, "Please select a location from the buttons above, or /cancel.")
			}
		case stateWaitingCamera:
			if sess.Camera == "__awaiting_text" && msg.Text != "" {
				sess.Camera = msg.Text
				sess.State = stateWaitingCaption
				saveSession()
				send(bot, msg.Chat.ID, "Add a caption (or /skip):")
			} else {
				send(bot, msg.Chat.ID, "Please select a camera from the buttons above, or /cancel.")
			}
		case stateWaitingCaption:
			handleCaption(bot, msg, cfg, store, ghSync)
		}
	}
}

func handlePhoto(bot *tgbotapi.BotAPI, msg *tgbotapi.Message, cfg *Config) {
	var fileID string
	if msg.Document != nil && strings.HasPrefix(msg.Document.MimeType, "image/") {
		fileID = msg.Document.FileID
	} else {
		fileID = msg.Photo[len(msg.Photo)-1].FileID
	}

	fileURL, err := bot.GetFileDirectURL(fileID)
	if err != nil {
		send(bot, msg.Chat.ID, "Failed to get photo. Try again.")
		return
	}

	resp, err := httpClient.Get(fileURL)
	if err != nil {
		send(bot, msg.Chat.ID, "Failed to download photo. Try again.")
		return
	}
	defer resp.Body.Close()

	tmpFile, err := os.CreateTemp("", "photo-*.jpg")
	if err != nil {
		send(bot, msg.Chat.ID, "Failed to save photo. Try again.")
		return
	}

	if _, err := io.Copy(tmpFile, io.LimitReader(resp.Body, 50<<20)); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		send(bot, msg.Chat.ID, "Failed to read photo. Try again.")
		return
	}
	tmpFile.Close()

	sess.PhotoPath = tmpFile.Name()
	sess.State = stateWaitingLocation
	saveSession()

	sendKeyboard(bot, msg.Chat.ID, "Select location:", makeTagKeyboard("loc", cfg.Tags.Locations))
}

func handleCallback(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery, cfg *Config) {
	bot.Request(tgbotapi.NewCallback(cb.ID, ""))
	chatID := cb.Message.Chat.ID

	switch {
	case sess.State == stateWaitingLocation && strings.HasPrefix(cb.Data, "loc:"):
		value := strings.TrimPrefix(cb.Data, "loc:")
		if value == "__custom" {
			sess.Location = "__awaiting_text"
			saveSession()
			send(bot, chatID, "Type the location name:")
			return
		}
		sess.Location = value
		sess.State = stateWaitingCamera
		saveSession()
		sendKeyboard(bot, chatID, "Select camera:", makeTagKeyboard("cam", cfg.Tags.Cameras))

	case sess.State == stateWaitingCamera && strings.HasPrefix(cb.Data, "cam:"):
		value := strings.TrimPrefix(cb.Data, "cam:")
		if value == "__custom" {
			sess.Camera = "__awaiting_text"
			saveSession()
			send(bot, chatID, "Type the camera name:")
			return
		}
		sess.Camera = value
		sess.State = stateWaitingCaption
		saveSession()
		send(bot, chatID, "Add a caption (or /skip):")
	}
}

func handleCaption(bot *tgbotapi.BotAPI, msg *tgbotapi.Message, cfg *Config, store *Storage, ghSync *GitHubSync) {
	if msg.Text == "" {
		send(bot, msg.Chat.ID, "Please type a caption or /skip.")
		return
	}

	caption := ""
	if !msg.IsCommand() || msg.Command() != "skip" {
		caption = msg.Text
	}

	photoData, err := os.ReadFile(sess.PhotoPath)
	if err != nil {
		send(bot, msg.Chat.ID, "Photo data lost. Please start over with a new photo.")
		resetSession()
		return
	}

	send(bot, msg.Chat.ID, "Uploading...")

	photoID := generateID()
	ctx := context.Background()

	origURL, err := store.Upload(ctx, fmt.Sprintf("originals/%s.jpg", photoID), photoData, "image/jpeg")
	if err != nil {
		log.Printf("Upload failed: %v", err)
		send(bot, msg.Chat.ID, "Upload failed. Try again or /cancel.")
		return
	}

	thumbData, err := MakeThumbnail(photoData, cfg.Thumbnail.Width, cfg.Thumbnail.Quality)
	if err != nil {
		log.Printf("Thumbnail failed: %v", err)
		send(bot, msg.Chat.ID, "Thumbnail failed. Try again or /cancel.")
		return
	}

	thumbURL, err := store.Upload(ctx, fmt.Sprintf("thumbs/%s.jpg", photoID), thumbData, "image/jpeg")
	if err != nil {
		log.Printf("Thumb upload failed: %v", err)
		send(bot, msg.Chat.ID, "Thumb upload failed. Try again or /cancel.")
		return
	}

	err = ghSync.AddPhoto(ctx, Photo{
		ID:       photoID,
		URL:      origURL,
		Thumb:    thumbURL,
		Caption:  caption,
		Location: sess.Location,
		Camera:   sess.Camera,
		Date:     time.Now().Format("2006-01-02"),
	})
	if err != nil {
		log.Printf("GitHub sync failed: %v", err)
		send(bot, msg.Chat.ID, "GitHub sync failed. Try again or /cancel.")
		return
	}

	text := "Published!\n\n"
	if caption != "" {
		text += caption + "\n"
	}
	text += sess.Location + " | " + sess.Camera
	send(bot, msg.Chat.ID, text)

	resetSession()
}

func makeTagKeyboard(prefix string, tags []string) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton
	for i := 0; i < len(tags); i += 2 {
		row := []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(tags[i], prefix+":"+tags[i]),
		}
		if i+1 < len(tags) {
			row = append(row, tgbotapi.NewInlineKeyboardButtonData(tags[i+1], prefix+":"+tags[i+1]))
		}
		rows = append(rows, row)
	}
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("Custom...", prefix+":__custom"),
	})
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}
