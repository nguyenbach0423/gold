package context

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/nguyenbach0423/gold/drive"
	"github.com/nguyenbach0423/gold/store"
	"github.com/nguyenbach0423/httpx/client"

	_ "modernc.org/sqlite"
)

type Context struct {
	Config             Config
	Drive              *drive.Drive
	Store              *store.Store
	TelegramHTTPClient client.Client
	CrawlerHTTPClient  client.Client
}

type Config struct {
	TimeLocation       *time.Location
	Server             ServerConfig
	SQLiteFilename     string
	SQLiteRemoteFileID string
	Bot                BotConfig
	NotifBatchSize     int
}

type ServerConfig struct {
	Port            string
	ShutdownTimeout time.Duration
	PublicURL       string
}

type BotConfig struct {
	Suffix                 string
	URL                    string
	FeedbackBotURL         string
	FeedbackReceiverChatID int
}

func New() (*Context, error) {
	env := os.Getenv("ENV")
	if env != "PROD" {
		if err := godotenv.Load(".env"); err != nil {
			return nil, err
		}
	}

	timeLocation, err := time.LoadLocation(os.Getenv("TIME_LOCATION"))
	if err != nil {
		return nil, err
	}

	shutdownTimeout, err := strconv.Atoi(os.Getenv("SHUTDOWN_TIMEOUT"))
	if err != nil {
		return nil, err
	}

	serverCfg := ServerConfig{
		Port:            os.Getenv("PORT"),
		ShutdownTimeout: time.Duration(shutdownTimeout) * time.Second,
		PublicURL:       os.Getenv("PUBLIC_URL"),
	}

	feedbackReceiverChatID, err := strconv.Atoi(os.Getenv("FEEDBACK_RECEIVER_CHAT_ID"))
	if err != nil {
		return nil, err
	}

	botCfg := BotConfig{
		Suffix:                 os.Getenv("BOT_SUFFIX"),
		URL:                    os.Getenv("BOT_URL"),
		FeedbackBotURL:         os.Getenv("FEEDBACK_BOT_URL"),
		FeedbackReceiverChatID: feedbackReceiverChatID,
	}

	if botCfg.Suffix == "" {
		botCfg.Suffix, err = gonanoid.New()
		if err != nil {
			return nil, err
		}
	}

	notifBatchSize, err := strconv.Atoi(os.Getenv("NOTIF_BATCH_SIZE"))
	if err != nil {
		return nil, err
	}

	cfg := Config{
		TimeLocation:       timeLocation,
		Server:             serverCfg,
		SQLiteFilename:     os.Getenv("SQLITE_FILENAME"),
		SQLiteRemoteFileID: os.Getenv("SQLITE_REMOTE_FILE_ID"),
		Bot:                botCfg,
		NotifBatchSize:     notifBatchSize,
	}

	d, err := drive.New(env, os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))
	if err != nil {
		return nil, err
	}

	if err = d.DownloadFile(cfg.SQLiteRemoteFileID, cfg.SQLiteFilename); err != nil {
		return nil, err
	}

	st, err := store.Open(func() (*store.SQLStore, error) {
		dsn := fmt.Sprintf("file:%s?_foreign_keys=ON&_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=%s",
			os.Getenv("SQLITE_FILENAME"),
			os.Getenv("SQLITE_BUSY_TIMEOUT"),
		)

		sqlStore, openErr := store.OpenSQLStore(
			os.Getenv("SQLITE_DRIVER_NAME"),
			dsn,
		)
		if openErr != nil {
			return nil, openErr
		}

		maxOpenConnections, _ := strconv.Atoi(os.Getenv("SQLITE_MAX_OPEN_CONNECTIONS"))
		if maxOpenConnections != 0 {
			sqlStore.SetMaxOpenConns(maxOpenConnections)
		}

		maxIdleConnections, _ := strconv.Atoi(os.Getenv("SQLITE_MAX_IDLE_CONNECTIONS"))
		if maxIdleConnections != 0 {
			sqlStore.SetMaxIdleConns(maxIdleConnections)
		}

		return sqlStore, nil
	})
	if err != nil {
		return nil, err
	}

	telegramHTTPClient := client.Client{
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		Headers: map[string]string{
			"User-Agent": "MrGoldVNBot/1.0 (+https://t.me/mr_gold_vn_bot)",
			"Accept":     "application/json",
			"Connection": "keep-alive",
		},
	}

	crawlerHTTPClient := client.Client{
		HTTPClient: &http.Client{
			Timeout: 45 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		},
		RetryConfig: &client.RetryConfig{
			MaxRetries: 5,
			Backoff:    500 * time.Millisecond,
			MaxBackoff: 5 * time.Second,
		},
		Headers: map[string]string{
			"User-Agent": "MrGoldVNBot/1.0 (+https://t.me/mr_gold_vn_bot)",
			"Accept":     "*/*",
			"Connection": "keep-alive",
		},
	}

	return &Context{
		Config:             cfg,
		Drive:              d,
		Store:              st,
		TelegramHTTPClient: telegramHTTPClient,
		CrawlerHTTPClient:  crawlerHTTPClient,
	}, nil
}

func (c *Context) Cancel() {
	if c.Store != nil {
		c.Store.Close()
	}
	if c.Drive != nil {
		err := c.Drive.UploadFile(c.Config.SQLiteFilename, c.Config.SQLiteRemoteFileID)
		if err != nil {
			fmt.Println(err)
		}
	}
}
