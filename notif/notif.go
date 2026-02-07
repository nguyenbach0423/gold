package notif

import (
	"database/sql"
	"fmt"
	"time"

	applicationcontext "github.com/nguyenbach0423/gold/context"
	"github.com/nguyenbach0423/gold/store"
	"github.com/nguyenbach0423/gold/telegram"
	"github.com/nguyenbach0423/workerpool"
	"github.com/rs/zerolog/log"
)

func SendVolatilityNotif(ctx *applicationcontext.Context, wp *workerpool.WorkerPool, bot *telegram.Telegram) {
	chatIDs, err := fetchVolatilityNotifSubcriptionChatIDs(ctx.Store)
	if err != nil {
		log.Error().Err(err).Send()
		return
	}

	sendNotif(ctx, wp, bot, chatIDs, "<b>📢 Thị trường có biến động mới, vào xem ngay nhé!</b>")
}

func SendScheduleNotif(ctx *applicationcontext.Context, wp *workerpool.WorkerPool, bot *telegram.Telegram) {
	now := time.Now().In(ctx.Config.TimeLocation)

	chatIDs, err := fetchScheduleNotifSubcriptionChatIDs(ctx.Store, now)
	if err != nil {
		log.Error().Err(err).Send()
		return
	}

	sendNotif(ctx, wp, bot, chatIDs, fmt.Sprintf("<b>📢 Giá vàng được cập nhật lúc %s</b>", now.Format("15:04 02/01/2006")))
}

func sendNotif(ctx *applicationcontext.Context, wp *workerpool.WorkerPool, bot *telegram.Telegram, chatIDs []int, title string) {
	if len(chatIDs) == 0 {
		return
	}

	batchSize := ctx.Config.NotifBatchSize
	for i := 0; i < len(chatIDs); i += batchSize {
		end := i + batchSize
		if end > len(chatIDs) {
			end = len(chatIDs)
		}

		batch := chatIDs[i:end]

		wp.Submit(func() {
			sendBatchNotif(bot, batch, title)
		})
	}
}

func sendBatchNotif(bot *telegram.Telegram, chatIDs []int, title string) {
	if len(chatIDs) == 0 {
		return
	}

	for _, chatID := range chatIDs {
		botMessage, err := bot.FindBotMessage(
			chatID,
			fmt.Sprintf("%s%s%s", telegram.InboxCode, telegram.Separator, title),
		)
		if err != nil {
			log.Error().Err(err).Send()
			continue
		}

		if botMessage == nil {
			continue
		}

		req := telegram.SendMessageRequest{
			ChatID:      chatID,
			ParseMode:   botMessage.ParseMode,
			Text:        botMessage.Text,
			ReplyMarkup: botMessage.ReplyMarkup,
		}

		if err = bot.SendMessage(req); err != nil {
			log.Error().Err(err).Send()
		}
	}
}

func fetchVolatilityNotifSubcriptionChatIDs(s *store.Store) ([]int, error) {
	var chatIDs []int

	if err := s.SQL.QueryRows(
		`select chat_id from volatility_notif_subcriptions where enabled = 1`,
		func(rows *sql.Rows) error {
			var chatID int

			if scanErr := rows.Scan(&chatID); scanErr != nil {
				return scanErr
			}

			chatIDs = append(chatIDs, chatID)

			return nil
		},
	); err != nil {
		return nil, err
	}

	return chatIDs, nil
}

func fetchScheduleNotifSubcriptionChatIDs(s *store.Store, now time.Time) ([]int, error) {
	latestNotif := genLatestNotif(now)

	var chatIDs []int

	filter := make(map[int]struct{})

	if err := s.SQL.QueryRows(
		`update schedule_notif_subcriptions
		set latest_notif = ?, enabled = (case when days_of_week = 0 then 0 else enabled end)
		where hour = ? and minute = ? and ((days_of_week & ?) != 0 or days_of_week = 0) and enabled = 1 and latest_notif < ?
		returning chat_id`,
		func(rows *sql.Rows) error {
			var chatID int
			if scanErr := rows.Scan(&chatID); scanErr != nil {
				return scanErr
			}

			if _, exist := filter[chatID]; !exist {
				filter[chatID] = struct{}{}
				chatIDs = append(chatIDs, chatID)
			}

			return nil
		},
		latestNotif, now.Hour(), now.Minute(), 1<<((now.Weekday()+6)%7), latestNotif,
	); err != nil {
		return nil, err
	}

	return chatIDs, nil
}

func genLatestNotif(t time.Time) int {
	return int(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).Unix())
}
