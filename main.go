package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	applicationcontext "github.com/nguyenbach0423/gold/context"
	"github.com/nguyenbach0423/gold/crawler"
	"github.com/nguyenbach0423/gold/notif"
	"github.com/nguyenbach0423/gold/telegram"
	"github.com/nguyenbach0423/gold/webhook/handler"
	webhookmiddleware "github.com/nguyenbach0423/gold/webhook/middleware"
	"github.com/nguyenbach0423/httpx/server"
	"github.com/nguyenbach0423/httpx/server/middleware"
	"github.com/nguyenbach0423/workerpool"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	appCtx, err := applicationcontext.New()
	if err != nil {
		log.Error().Err(err).Send()
		return
	}
	defer appCtx.Cancel()

	bot, err := initBot(appCtx, appCtx.Config.Server.PublicURL+appCtx.Config.Bot.Suffix)
	if err != nil {
		log.Error().Err(err).Send()
		return
	}

	cr := crawler.Crawler{Ctx: appCtx}

	c := cron.New(
		cron.WithSeconds(),
		cron.WithLocation(appCtx.Config.TimeLocation),
		cron.WithChain(
			cron.Recover(cron.DefaultLogger),
			cron.DelayIfStillRunning(cron.DefaultLogger),
		),
	)

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	wp := workerpool.New(signalCtx, 10)
	defer wp.Wait()

	wp.Submit(func() {
		cr.Run(wp, bot)
	})

	_, err = c.AddFunc("@every 5m", func() {
		uploadFileErr := appCtx.Drive.UploadFile(appCtx.Config.SQLiteFilename, appCtx.Config.SQLiteRemoteFileID)
		if uploadFileErr != nil {
			log.Error().Err(uploadFileErr).Send()
		}
	})
	if err != nil {
		log.Error().Err(err).Send()
		stop()
	}

	_, err = c.AddFunc("0 * * * * *", func() {
		notif.SendScheduleNotif(appCtx, wp, bot)
	})
	if err != nil {
		log.Error().Err(err).Send()
		stop()
	}

	_, err = c.AddFunc("@every 5m", func() {
		cr.Run(wp, bot)
	})
	if err != nil {
		log.Error().Err(err).Send()
		stop()
	}

	c.Start()

	s := server.New(server.WithHealthCheck())

	s.Use(webhookmiddleware.Logger(), middleware.Recover())

	s.Post(
		appCtx.Config.Bot.Suffix,
		handler.BotHandler,
		middleware.WithParams(
			map[string]any{
				"bot":        bot,
				"workerpool": wp,
			},
		),
	)

	go func() {
		if listenAndServeErr := s.ListenAndServe(appCtx.Config.Server.Port); listenAndServeErr != nil {
			log.Error().Err(listenAndServeErr).Send()
			stop()
		}
	}()

	<-signalCtx.Done()

	cronCtx := c.Stop()
	<-cronCtx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), appCtx.Config.Server.ShutdownTimeout)
	defer cancel()

	if shutdownErr := s.Shutdown(shutdownCtx); shutdownErr != nil {
		log.Error().Err(shutdownErr).Send()
	}
}

func init() {
	consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout, NoColor: true, TimeFormat: time.DateTime}
	log.Logger = log.Output(consoleWriter)
}

func initBot(ctx *applicationcontext.Context, botWebhookURL string) (*telegram.Telegram, error) {
	t := &telegram.Telegram{
		Ctx: ctx,
		URL: ctx.Config.Bot.URL,
	}

	if err := t.HandleWebhook(botWebhookURL); err != nil {
		return nil, err
	}

	log.Info().Str("bot_url", botWebhookURL).Send()

	if err := t.HandleBotCommands(telegram.BotCommands...); err != nil {
		return nil, err
	}

	return t, nil
}
