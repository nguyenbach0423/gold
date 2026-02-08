package telegram

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nguyenbach0423/gold/context"
	"github.com/nguyenbach0423/gold/store"
	"github.com/nguyenbach0423/httpx/client/request"
	"github.com/nguyenbach0423/workerpool"
	"github.com/rs/zerolog/log"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"modernc.org/sqlite"
)

type Telegram struct {
	Ctx *context.Context
	URL string
}

func (t *Telegram) request(path string, v any) error {
	reqBody, err := json.Marshal(v)
	if err != nil {
		return err
	}

	_, err = t.Ctx.TelegramHTTPClient.Do(
		&request.Request{
			Method: http.MethodPost,
			URL:    t.URL + path,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: reqBody,
		},
	)

	return err
}

func (t *Telegram) setWebhook(req SetWebhookRequest) error {
	return t.request("/setWebhook", req)
}

func (t *Telegram) setMyCommands(req SetMyCommandsRequest) error {
	return t.request("/setMyCommands", req)
}

func (t *Telegram) SendMessage(req SendMessageRequest) error {
	return t.request("/sendMessage", req)
}

func (t *Telegram) answerCallbackQuery(req AnswerCallbackQueryRequest) error {
	return t.request("/answerCallbackQuery", req)
}

func (t *Telegram) editMessageText(req EditMessageTextRequest) error {
	return t.request("/editMessageText", req)
}

func (t *Telegram) sendPhoto(req SendPhotoRequest) error {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	err := writer.WriteField("chat_id", strconv.FormatInt(int64(req.ChatID), 10))
	if err != nil {
		return err
	}

	if req.ParseMode != "" {
		err = writer.WriteField("parse_mode", req.ParseMode)
		if err != nil {
			return err
		}
	}

	if req.Caption != "" {
		err = writer.WriteField("caption", req.Caption)
		if err != nil {
			return err
		}
	}

	part, _ := writer.CreateFormFile("photo", req.Filename)
	_, err = part.Write(req.Photo)
	if err != nil {
		return err
	}

	err = writer.Close()
	if err != nil {
		log.Error().Err(err).Send()
	}

	_, err = t.Ctx.TelegramHTTPClient.Do(
		&request.Request{
			Method: http.MethodPost,
			URL:    t.URL + "/sendPhoto",
			Headers: map[string]string{
				"Content-Type": writer.FormDataContentType(),
			},
			Body: buf.Bytes(),
		},
	)
	if err != nil {
		return err
	}

	return nil
}

func (t *Telegram) HandleWebhook(url string) error {
	req := SetWebhookRequest{
		URL: url,
	}

	return t.setWebhook(req)
}

var (
	StartCommand = BotCommand{
		Command: "/start",
	}
	LiveCommand = BotCommand{
		Command:     "/live",
		Description: "Cập nhật giá vàng mới nhất",
	}
	NotifCommand = BotCommand{
		Command:     "/notif",
		Description: "Thiết lập thông báo giá vàng",
	}
	HistoryCommand = BotCommand{
		Command:     "/history",
		Description: "Tra cứu lịch sử giá vàng",
	}
	FeedbackCommand = BotCommand{
		Command:     "/feedback",
		Description: "Gửi góp ý cải thiện Bot",
	}
	DonateCommand = BotCommand{
		Command:     "/donate",
		Description: "☕︎ Buy me a coffee",
	}
)

const (
	Feedback = "/send"
)

var BotCommands = []BotCommand{
	LiveCommand,
	NotifCommand,
	HistoryCommand,
	FeedbackCommand,
	DonateCommand,
}

func (t *Telegram) HandleBotCommands(commands ...BotCommand) error {
	req := SetMyCommandsRequest{
		Commands: commands,
	}

	return t.setMyCommands(req)
}

func (t *Telegram) HandleUpdate(wp *workerpool.WorkerPool, update *Update) {
	if update == nil {
		return
	}

	if update.MyChatMember != nil {
		t.handleMyChatMember(update.MyChatMember.Chat)
	}

	if update.Message != nil {
		t.handleMessage(wp, update.Message)
	} else if update.CallbackQuery != nil {
		t.handleCallbackQuery(wp, update.CallbackQuery)
	}
}

const (
	IntroCode            = "000"
	LiveCode             = "100"
	NotifCode            = "200"
	VolatilityCode       = "201"
	SchedulesCode        = "202"
	ScheduleCode         = "203"
	SelectHourCode       = "204"
	SelectMinuteCode     = "205"
	SelectDaysOfWeekCode = "206"
	ConfirmScheduleCode  = "207"
	DeleteScheduleCode   = "208"
	HistoryCode          = "300"
	GoldPriceHistoryCode = "301"
	FeedbackCode         = "400"
	DonateCode           = "500"
	InboxCode            = "600"
)

const (
	Separator = "|"
)

func (t *Telegram) handleMessage(wp *workerpool.WorkerPool, message *Message) {
	wp.Submit(func() {
		text := strings.ToLower(strings.TrimSpace(message.Text))
		if strings.HasPrefix(text, Feedback) {
			t.handleFeedback(message.Chat, text)
			return
		}

		var code string

		if strings.HasPrefix(text, StartCommand.Command) {
			t.handStartCommand(message.Chat)
			code = IntroCode
		} else if strings.HasPrefix(text, LiveCommand.Command) {
			code = LiveCode
		} else if strings.HasPrefix(text, NotifCommand.Command) {
			code = NotifCode
		} else if strings.HasPrefix(text, HistoryCommand.Command) {
			code = HistoryCode
		} else if strings.HasPrefix(text, FeedbackCommand.Command) {
			code = FeedbackCode
		} else if strings.HasPrefix(text, DonateCommand.Command) {
			code = DonateCode
		} else {
			code = IntroCode
		}

		botMessage, err := t.FindBotMessage(message.Chat.ID, code)
		if err != nil {
			log.Error().Err(err).Send()
			return
		}

		if botMessage == nil {
			return
		}

		req := SendMessageRequest{
			ChatID:      message.Chat.ID,
			ParseMode:   botMessage.ParseMode,
			Text:        botMessage.Text,
			ReplyMarkup: botMessage.ReplyMarkup,
		}

		if err = t.SendMessage(req); err != nil {
			log.Error().Err(err).Send()
		}
	})
}

func (t *Telegram) handleMyChatMember(chat Chat) {
	if err := t.saveChat(chat); err != nil {
		log.Error().Err(err).Send()
	}

	builder := strings.Builder{}

	builder.WriteString(fmt.Sprintf("<b>🖐🏻 🐶 Cậu Vàng xin chào nhóm"))
	if chat.Title != "" {
		builder.WriteString(fmt.Sprintf(" %s", chat.Title))
	}

	builder.WriteString("\n\nRất vui được đồng hành cùng các thành viên!</b>")

	if err := t.SendMessage(SendMessageRequest{
		ChatID:    chat.ID,
		ParseMode: "HTML",
		Text:      builder.String(),
	}); err != nil {
		log.Error().Err(err).Send()
	}
}

func (t *Telegram) handStartCommand(chat Chat) {
	if chat.Type == "private" {
		if err := t.saveChat(chat); err != nil {
			log.Error().Err(err).Send()
		}

		builder := strings.Builder{}

		builder.WriteString(fmt.Sprintf("<b>🖐🏻 Chào mừng"))
		if chat.FirstName != "" {
			builder.WriteString(fmt.Sprintf(" %s", chat.FirstName))
		}
		if chat.LastName != "" {
			builder.WriteString(fmt.Sprintf(" %s", chat.LastName))
		}

		builder.WriteString(" đến với 🐶 Cậu Vàng\n\nRất vui được đồng hành cùng bạn!</b>")

		if err := t.SendMessage(SendMessageRequest{
			ChatID:    chat.ID,
			ParseMode: "HTML",
			Text:      builder.String(),
		}); err != nil {
			log.Error().Err(err).Send()
		}
	}
}

func (t *Telegram) handleFeedback(chat Chat, text string) {
	if err := t.SendMessage(SendMessageRequest{
		ChatID:    chat.ID,
		ParseMode: "HTML",
		Text:      "<b>🤝 Cảm ơn góp ý của bạn!</b>",
	}); err != nil {
		log.Error().Err(err).Send()
	}

	feedback := fmt.Sprintf("<b>chat_id: %d", chat.ID)
	if chat.Username != "" {
		feedback += fmt.Sprintf("\n\nusername: %s", chat.Username)
	}
	if chat.FirstName != "" {
		feedback += fmt.Sprintf("\n\nfirst_name: %s", chat.FirstName)
	}
	if chat.LastName != "" {
		feedback += fmt.Sprintf("\n\nlast_name: %s", chat.LastName)
	}
	if chat.Title != "" {
		feedback += fmt.Sprintf("\n\ntitle: %s", chat.Title)
	}
	feedback += fmt.Sprintf("\n\nfeedback: %s</b>", strings.TrimSpace(strings.TrimPrefix(text, Feedback)))

	t.sendFeedback(feedback)
}

func (t *Telegram) handleCallbackQuery(wp *workerpool.WorkerPool, callbackQuery *CallbackQuery) {
	wp.Submit(func() {
		if err := t.answerCallbackQuery(
			AnswerCallbackQueryRequest{
				CallbackQueryID: callbackQuery.ID,
			},
		); err != nil {
			log.Error().Err(err).Send()
		}
	})

	wp.Submit(func() {
		botMessage, err := t.FindBotMessage(callbackQuery.Message.Chat.ID, callbackQuery.Data)
		if err != nil {
			log.Error().Err(err).Send()
			return
		}

		if botMessage == nil {
			return
		}

		req := EditMessageTextRequest{
			ChatID:      callbackQuery.Message.Chat.ID,
			MessageID:   callbackQuery.Message.ID,
			ParseMode:   botMessage.ParseMode,
			Text:        botMessage.Text,
			ReplyMarkup: botMessage.ReplyMarkup,
		}

		if err = t.editMessageText(req); err != nil {
			log.Error().Err(err).Send()
		}
	})
}

func (t *Telegram) saveChat(chat Chat) error {
	return t.Ctx.Store.SQL.WithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`insert or ignore into chats (id, type, username, first_name, last_name, title)
			values (?, ?, ?, ?, ?, ?)`,
			chat.ID, chat.Type, chat.Username, chat.FirstName, chat.LastName, chat.Title,
		)

		return err
	})
}

func (t *Telegram) FindBotMessage(chatID int, code string) (*BotMessage, error) {
	parts := strings.Split(code, Separator)

	if len(parts) == 0 {
		return nil, nil
	}

	fn, exist := fns[parts[0]]
	if !exist {
		return nil, nil
	}

	return fn(t.Ctx, chatID, parts[1:]...)
}

var IntroFunc = func(ctx *context.Context, id int, params ...string) (*BotMessage, error) {
	return &BotMessage{
		ParseMode: "HTML",
		Text:      "<b>🐶 Cậu Vàng - Trợ lý thông minh</b>",
		ReplyMarkup: &ReplyMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				{
					{
						Text:         LiveCommand.Description,
						CallbackData: LiveCode,
					},
				},
				{
					{
						Text:         NotifCommand.Description,
						CallbackData: NotifCode,
					},
				},
				{
					{
						Text:         HistoryCommand.Description,
						CallbackData: HistoryCode,
					},
				},
			},
		},
	}, nil
}

var LiveFunc = func(ctx *context.Context, chatID int, params ...string) (*BotMessage, error) {
	text, err := board(ctx, "<b>Giá vàng được cập nhật liên tục theo thị trường</b>")
	if err != nil {
		return nil, err
	}

	if text == "" {
		return nil, nil
	}

	return &BotMessage{
		ParseMode: "HTML",
		Text:      text,
		ReplyMarkup: &ReplyMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				{
					{
						Text:         "<< Quay lại",
						CallbackData: IntroCode,
					},
					{
						Text:         "Cập nhật",
						CallbackData: LiveCode,
					},
				},
			},
		},
	}, nil
}

var NotifFunc = func(ctx *context.Context, chatID int, params ...string) (*BotMessage, error) {
	return &BotMessage{
		ParseMode: "HTML",
		Text:      "<b>Thiết lập thông báo giá vàng để nhận thông tin sớm nhất</b>",
		ReplyMarkup: &ReplyMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				{
					{
						Text:         "Nhận thông báo khi giá vàng biến động",
						CallbackData: VolatilityCode,
					},
				},
				{
					{
						Text:         "Đặt lịch thông báo hàng ngày",
						CallbackData: SchedulesCode,
					},
				},
				{
					{
						Text:         "<< Quay lại",
						CallbackData: IntroCode,
					},
				},
			},
		},
	}, nil
}

var VolatilityFunc = func(ctx *context.Context, chatID int, params ...string) (*BotMessage, error) {
	var enabled bool
	var err error

	if len(params) == 0 {
		if enabled, err = isVolatilityNotifEnabled(ctx.Store, chatID); err != nil {
			return nil, err
		}
	} else {

		enabled, err = strconv.ParseBool(params[0])
		if err != nil {
			return nil, err
		}

		if err = saveVolatilityNotifSubcription(ctx.Store, chatID, enabled); err != nil {
			return nil, err
		}
	}

	builder := strings.Builder{}

	builder.WriteString("<b>Nhận thông báo khi giá vàng biến động</b>")

	switchButton := InlineKeyboardButton{
		Text:         "Bật thông báo",
		CallbackData: VolatilityCode + Separator + strconv.FormatBool(true),
	}

	if enabled {
		builder.WriteString("\n\n🔔   <b><i>Thông báo đang bật</i></b>")

		switchButton.Text = "Tắt thông báo"
		switchButton.CallbackData = VolatilityCode + Separator + strconv.FormatBool(false)
	} else {
		builder.WriteString("\n\n🔕   <b><i>Thông báo đang tắt</i></b>")
	}

	inlineKeyboard := [][]InlineKeyboardButton{
		{
			{
				Text:         "<< Quay lại",
				CallbackData: NotifCode,
			},
			switchButton,
		},
	}

	return &BotMessage{
		ParseMode: "HTML",
		Text:      builder.String(),
		ReplyMarkup: &ReplyMarkup{
			InlineKeyboard: inlineKeyboard,
		},
	}, nil
}

var SchedulesFunc = func(ctx *context.Context, chatID int, params ...string) (*BotMessage, error) {
	subcriptions, err := fetchScheduleNotifSubcriptions(ctx.Store, chatID)
	if err != nil {
		return nil, err
	}

	var inlineKeyboard [][]InlineKeyboardButton

	for _, subcription := range subcriptions {
		schedule := encodeSchedule(subcription.Hour, subcription.Minute, subcription.DaysOfWeek)

		builder := strings.Builder{}

		if subcription.Enabled {
			builder.WriteString("🔔   ")
		} else {
			builder.WriteString("🔕   ")
		}

		builder.WriteString(fmt.Sprintf("%02d:%02d", subcription.Hour, subcription.Minute))

		if subcription.DaysOfWeek != 0 {
			builder.WriteString(fmt.Sprintf(" - %s", formatDaysOfWeek(subcription.DaysOfWeek)))
		}

		inlineKeyboard = append(inlineKeyboard, []InlineKeyboardButton{
			{
				Text:         builder.String(),
				CallbackData: fmt.Sprintf("%s%s%d", ScheduleCode, Separator, schedule),
			},
		})
	}

	inlineKeyboard = append(inlineKeyboard, []InlineKeyboardButton{
		{
			Text:         "<< Quay lại",
			CallbackData: NotifCode,
		},
		{
			Text:         "Thêm mới",
			CallbackData: fmt.Sprintf("%s%s%d%s%d%s%s%s%d", SelectHourCode, Separator, 0, Separator, 0, Separator, strconv.FormatBool(true), Separator, 0),
		},
	})

	return &BotMessage{
		ParseMode: "HTML",
		Text:      "<b>Đặt lịch thông báo hàng ngày</b>",
		ReplyMarkup: &ReplyMarkup{
			InlineKeyboard: inlineKeyboard,
		},
	}, nil
}

var ScheduleFunc = func(ctx *context.Context, chatID int, params ...string) (*BotMessage, error) {
	if len(params) < 1 {
		return nil, nil
	}

	mask, err := strconv.Atoi(params[0])
	if err != nil {
		return nil, err
	}

	hour, minute, daysOfWeek := decodeSchedule(mask)

	subcription, err := findScheduleNotifSubcription(ctx.Store, chatID, hour, minute, daysOfWeek)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SchedulesFunc(ctx, chatID)
		}
		return nil, err
	}

	builder := strings.Builder{}

	builder.WriteString(fmt.Sprintf("<b>Lịch thông báo: <i>%02d:%02d</i></b>", hour, minute))
	if daysOfWeek != 0 {
		builder.WriteString(fmt.Sprintf("<b><i> - %s</i></b>", formatDaysOfWeek(daysOfWeek)))
	}

	if len(params) > 1 {
		enabled, err := strconv.ParseBool(params[1])
		if err != nil {
			return nil, err
		}

		subcription.Enabled = enabled

		if err = toggleScheduleNotifSubcriptionEnabled(ctx.Store, subcription); err != nil {
			return nil, err
		}
	}

	switchButton := InlineKeyboardButton{
		Text:         "Bật thông báo",
		CallbackData: fmt.Sprintf("%s%s%d%s%s", ScheduleCode, Separator, mask, Separator, strconv.FormatBool(!subcription.Enabled)),
	}

	if subcription.Enabled {
		switchButton.Text = "Tắt thông báo"

		builder.WriteString("\n\n🔔   <b><i>Thông báo đang bật</i></b>")
	} else {
		builder.WriteString("\n\n🔕   <b><i>Thông báo đang tắt</i></b>")
	}

	return &BotMessage{
		ParseMode: "HTML",
		Text:      builder.String(),
		ReplyMarkup: &ReplyMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				{
					{
						Text:         "Xóa",
						CallbackData: fmt.Sprintf("%s%s%d", DeleteScheduleCode, Separator, mask),
					},
					{
						Text:         "Sửa",
						CallbackData: fmt.Sprintf("%s%s%d%s%d%s%s%s%d", SelectHourCode, Separator, mask, Separator, mask, Separator, strconv.FormatBool(false), Separator, hour/6),
					},
				},
				{
					{
						Text:         "<< Quay lại",
						CallbackData: SchedulesCode,
					},
					switchButton,
				},
			},
		},
	}, nil
}

var SelectHourFunc = func(ctx *context.Context, chatID int, params ...string) (*BotMessage, error) {
	saved, unsaved, isNew, index, err := handleParams(4, params...)
	if err != nil {
		return nil, err
	}

	hour, minute, daysOfWeek := decodeSchedule(unsaved)

	builder := strings.Builder{}

	builder.WriteString("<b>Chọn giờ</b>")
	builder.WriteString(fmt.Sprintf("\n\n<b><i>(%02d):%02d</i></b>", hour, minute))
	if daysOfWeek != 0 {
		builder.WriteString(fmt.Sprintf("<b><i> - %s</i></b>", formatDaysOfWeek(daysOfWeek)))
	}

	var inlineKeyboard [][]InlineKeyboardButton

	inlineKeyboard = append(inlineKeyboard, page(SelectHourCode, saved, unsaved, index, 4, isNew))

	cancelButton := InlineKeyboardButton{
		Text:         "Hủy bỏ",
		CallbackData: fmt.Sprintf("%s%s%d", ScheduleCode, Separator, saved),
	}

	if isNew {
		cancelButton.CallbackData = SchedulesCode
	}

	inlineKeyboard = append(inlineKeyboard, []InlineKeyboardButton{
		cancelButton,
		{
			Text:         "Tiếp theo >>",
			CallbackData: fmt.Sprintf("%s%s%d%s%d%s%s%s%d", SelectMinuteCode, Separator, saved, Separator, unsaved, Separator, strconv.FormatBool(isNew), Separator, minute/6),
		},
	})

	return &BotMessage{
		ParseMode: "HTML",
		Text:      builder.String(),
		ReplyMarkup: &ReplyMarkup{
			InlineKeyboard: inlineKeyboard,
		},
	}, nil
}

var SelectMinuteFunc = func(ctx *context.Context, chatID int, params ...string) (*BotMessage, error) {
	saved, unsaved, isNew, index, err := handleParams(4, params...)
	if err != nil {
		return nil, err
	}

	hour, minute, daysOfWeek := decodeSchedule(unsaved)

	builder := strings.Builder{}

	builder.WriteString("<b>Chọn phút</b>")
	builder.WriteString(fmt.Sprintf("\n\n<b><i>%02d:(%02d)</i></b>", hour, minute))
	if daysOfWeek != 0 {
		builder.WriteString(fmt.Sprintf("<b><i> - %s</i></b>", formatDaysOfWeek(daysOfWeek)))
	}

	var inlineKeyboard [][]InlineKeyboardButton

	inlineKeyboard = append(inlineKeyboard, page(SelectMinuteCode, saved, unsaved, index, 10, isNew))

	cancelButton := InlineKeyboardButton{
		Text:         "Hủy bỏ",
		CallbackData: fmt.Sprintf("%s%s%d", ScheduleCode, Separator, saved),
	}

	if isNew {
		cancelButton.CallbackData = SchedulesCode
	}

	inlineKeyboard = append(inlineKeyboard, []InlineKeyboardButton{
		{
			Text:         "<< Quay lại",
			CallbackData: fmt.Sprintf("%s%s%d%s%d%s%s%s%d", SelectHourCode, Separator, saved, Separator, unsaved, Separator, strconv.FormatBool(isNew), Separator, hour/6),
		},
		cancelButton,
		{
			Text:         "Tiếp theo >>",
			CallbackData: fmt.Sprintf("%s%s%d%s%d%s%s", SelectDaysOfWeekCode, Separator, saved, Separator, unsaved, Separator, strconv.FormatBool(isNew)),
		},
	})

	return &BotMessage{
		ParseMode: "HTML",
		Text:      builder.String(),
		ReplyMarkup: &ReplyMarkup{
			InlineKeyboard: inlineKeyboard,
		},
	}, nil
}

var SelectDaysOfWeekFunc = func(ctx *context.Context, chatID int, params ...string) (*BotMessage, error) {
	saved, unsaved, isNew, _, err := handleParams(3, params...)
	if err != nil {
		return nil, err
	}

	hour, minute, daysOfWeek := decodeSchedule(unsaved)

	builder := strings.Builder{}

	builder.WriteString("<b>Lặp lại?</b>")
	builder.WriteString(fmt.Sprintf("\n\n<b><i>%02d:%02d</i></b>", hour, minute))
	if daysOfWeek != 0 {
		builder.WriteString(fmt.Sprintf("<b><i> - (%s)</i></b>", formatDaysOfWeek(daysOfWeek)))
	}

	var inlineKeyboard [][]InlineKeyboardButton

	days := make([]int, 0, len(DaysOfWeek))
	for day := range DaysOfWeek {
		days = append(days, day)
	}
	sort.Ints(days)

	var dayButtons []InlineKeyboardButton

	for _, day := range days {
		prefix := ""
		if daysOfWeek&day != 0 {
			prefix = "✦ "
		}

		text := fmt.Sprintf("%s%s", prefix, DaysOfWeek[day])

		dayButtons = append(dayButtons, InlineKeyboardButton{
			Text:         text,
			CallbackData: fmt.Sprintf("%s%s%d%s%d%s%s", SelectDaysOfWeekCode, Separator, saved, Separator, encodeSchedule(hour, minute, daysOfWeek^day), Separator, strconv.FormatBool(isNew)),
		})
	}

	inlineKeyboard = append(inlineKeyboard, dayButtons)

	cancelButton := InlineKeyboardButton{
		Text:         "Hủy bỏ",
		CallbackData: fmt.Sprintf("%s%s%d", ScheduleCode, Separator, saved),
	}

	if isNew {
		cancelButton.CallbackData = SchedulesCode
	}

	controlButtons := []InlineKeyboardButton{
		{
			Text:         "<< Quay lại",
			CallbackData: fmt.Sprintf("%s%s%d%s%d%s%s%s%d", SelectMinuteCode, Separator, saved, Separator, unsaved, Separator, strconv.FormatBool(isNew), Separator, minute/6),
		},
		cancelButton,
	}

	if isNew || (saved != unsaved) {
		controlButtons = append(controlButtons, InlineKeyboardButton{
			Text:         "Hoàn thành",
			CallbackData: fmt.Sprintf("%s%s%d%s%d%s%s", ConfirmScheduleCode, Separator, saved, Separator, unsaved, Separator, strconv.FormatBool(isNew)),
		})
	}

	inlineKeyboard = append(inlineKeyboard, controlButtons)

	return &BotMessage{
		ParseMode: "HTML",
		Text:      builder.String(),
		ReplyMarkup: &ReplyMarkup{
			InlineKeyboard: inlineKeyboard,
		},
	}, nil
}

var ConfirmScheduleFunc = func(ctx *context.Context, chatID int, params ...string) (*BotMessage, error) {
	saved, unsaved, isNew, _, err := handleParams(3, params...)
	if err != nil {
		return nil, err
	}

	savedHour, savedMinute, savedDaysOfWeek := decodeSchedule(saved)

	savedSubcription := &store.ScheduleNotifSubcription{
		ChatID:     chatID,
		Hour:       savedHour,
		Minute:     savedMinute,
		DaysOfWeek: savedDaysOfWeek,
	}

	unsavedHour, unsavedMinute, unsavedDaysOfWeek := decodeSchedule(unsaved)

	unsavedSubcription := &store.ScheduleNotifSubcription{
		ChatID:     chatID,
		Hour:       unsavedHour,
		Minute:     unsavedMinute,
		DaysOfWeek: unsavedDaysOfWeek,
	}

	if isNew {
		err = insertScheduleNotifSubcription(ctx.Store, unsavedSubcription)
	} else {
		err = saveScheduleNotifSubcription(ctx.Store, unsavedSubcription, savedSubcription)
	}

	builder := strings.Builder{}

	if err != nil {
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && (sqliteErr.Code() == 1555 || sqliteErr.Code() == 2067) {
			if isNew {
				builder.WriteString("<b>Lịch thông báo đã tồn tại</b>")
			} else {
				builder.WriteString("<b>Lịch thông báo bị trùng, không thể sửa</b>")
			}
		} else {
			log.Error().Err(err).Send()

			if isNew {
				builder.WriteString("<b>Đã xảy ra lỗi khi thêm mới lịch thông báo</b>")
			} else {
				builder.WriteString("<b>Đã xảy ra lỗi khi sửa lịch thông báo</b>")
			}
		}

		builder.WriteString(fmt.Sprintf("\n\n<b><i>%02d:%02d - %s</i></b>", unsavedHour, unsavedMinute, formatDaysOfWeek(unsavedDaysOfWeek)))

		cancelButton := InlineKeyboardButton{
			Text:         "Hủy bỏ",
			CallbackData: fmt.Sprintf("%s%s%d", ScheduleCode, Separator, saved),
		}

		if isNew {
			cancelButton.CallbackData = SchedulesCode
		}

		return &BotMessage{
			ParseMode: "HTML",
			Text:      builder.String(),
			ReplyMarkup: &ReplyMarkup{
				InlineKeyboard: [][]InlineKeyboardButton{
					{
						{
							Text:         "<< Quay lại",
							CallbackData: fmt.Sprintf("%s%s%d%s%d", SelectDaysOfWeekCode, Separator, saved, Separator, unsaved),
						},
						cancelButton,
					},
				},
			},
		}, nil
	}

	if isNew {
		builder.WriteString("<b>Đã thêm mới lịch thông báo</b>")

		builder.WriteString("\n\n")
	} else {
		builder.WriteString("<b>Đã sửa lịch thông báo</b>")

		builder.WriteString(fmt.Sprintf("\n\n<b><i><s>%02d:%02d</s></i></b>", savedHour, savedMinute))
		if savedDaysOfWeek != 0 {
			builder.WriteString(fmt.Sprintf("<b><i><s> - %s</s></i></b>", formatDaysOfWeek(savedDaysOfWeek)))
		}

		builder.WriteString("\n")
	}

	builder.WriteString(fmt.Sprintf("<b><i>%02d:%02d</i></b>", unsavedHour, unsavedMinute))
	if unsavedDaysOfWeek != 0 {
		builder.WriteString(fmt.Sprintf("<b><i> - %s</i></b>", formatDaysOfWeek(unsavedDaysOfWeek)))
	}

	return &BotMessage{
		ParseMode: "HTML",
		Text:      builder.String(),
		ReplyMarkup: &ReplyMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				{
					{
						Text:         "Xong",
						CallbackData: SchedulesCode,
					},
				},
			},
		},
	}, nil
}

var DeleteScheduleFunc = func(ctx *context.Context, chatID int, params ...string) (*BotMessage, error) {
	mask, _, _, _, err := handleParams(1, params...)
	if err != nil {
		return nil, err
	}

	hour, minute, daysOfWeek := decodeSchedule(mask)

	if err := deleteScheduleNotifSubcription(ctx.Store, &store.ScheduleNotifSubcription{
		ChatID:     chatID,
		Hour:       hour,
		Minute:     minute,
		DaysOfWeek: daysOfWeek,
	}); err != nil {
		log.Error().Err(err).Send()

		builder := strings.Builder{}

		builder.WriteString("<b>Đã xảy ra lỗi khi xóa lịch thông báo</b>")
		builder.WriteString(fmt.Sprintf("\n\n<b><i><s>%02d:%02d</s></i></b>", hour, minute))

		if daysOfWeek != 0 {
			builder.WriteString(fmt.Sprintf("<b><i><s> - %s</s></i></b>", formatDaysOfWeek(daysOfWeek)))
		}

		return &BotMessage{
			ParseMode: "HTML",
			Text:      builder.String(),
			ReplyMarkup: &ReplyMarkup{
				InlineKeyboard: [][]InlineKeyboardButton{
					{
						{
							Text:         "<< Quay lại",
							CallbackData: fmt.Sprintf("%s%s%d", ScheduleCode, Separator, mask),
						},
					},
				},
			},
		}, nil
	}

	builder := strings.Builder{}

	builder.WriteString("<b>Đã xóa lịch thông báo</b>")
	builder.WriteString(fmt.Sprintf("\n\n<b><i><s>%02d:%02d</s></i></b>", hour, minute))
	if daysOfWeek != 0 {
		builder.WriteString(fmt.Sprintf("<b><i><s> - %s</s></i></b>", formatDaysOfWeek(daysOfWeek)))
	}

	return &BotMessage{
		ParseMode: "HTML",
		Text:      builder.String(),
		ReplyMarkup: &ReplyMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				{
					{
						Text:         "Xong",
						CallbackData: SchedulesCode,
					},
				},
			},
		},
	}, nil
}

var HistoryFunc = func(ctx *context.Context, chatID int, params ...string) (*BotMessage, error) {
	golds, err := fetchGolds(ctx.Store)
	if err != nil {
		return nil, err
	}

	if len(golds) == 0 {
		return nil, nil
	}

	inlineKeyboard := make([][]InlineKeyboardButton, 0, len(golds))

	for _, gold := range golds {
		inlineKeyboard = append(inlineKeyboard, []InlineKeyboardButton{
			{
				Text:         gold.Name,
				CallbackData: fmt.Sprintf("%s%s%d%s%s", GoldPriceHistoryCode, Separator, gold.ID, Separator, Day),
			},
		})
	}

	inlineKeyboard = append(inlineKeyboard, []InlineKeyboardButton{
		{
			Text:         "<< Quay lại",
			CallbackData: IntroCode,
		},
	})

	return &BotMessage{
		ParseMode: "HTML",
		Text:      "<b>Chọn sản phẩm để xem lịch sử giá vàng</b>",
		ReplyMarkup: &ReplyMarkup{
			InlineKeyboard: inlineKeyboard,
		},
	}, nil
}

var GoldPriceHistoryFunc = func(ctx *context.Context, chatID int, params ...string) (*BotMessage, error) {
	if len(params) != 2 {
		return nil, nil
	}

	goldID, err := strconv.Atoi(params[0])
	if err != nil {
		return nil, err
	}

	timeRange := params[1]
	if !slices.Contains(TimeRanges, timeRange) {
		return nil, nil
	}

	t := time.Now().In(ctx.Config.TimeLocation)

	goldPrices, err := fetchGoldPriceHistory(ctx.Store, goldID, timeRange, t)
	if err != nil {
		return nil, err
	}

	if len(goldPrices) == 0 {
		return nil, nil
	}

	priceDate := t.Format(time.DateOnly)
	if goldPrices[0].PriceDate != priceDate {
		if err = HandleNewDay(ctx); err != nil {
			return nil, err
		}

		goldPrice, err := findGoldPriceHistory(ctx.Store, goldID, priceDate)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, nil
			}
			return nil, err
		}

		goldPrices = append([]GoldPriceHistory{*goldPrice}, goldPrices...)
	}

	builder := strings.Builder{}

	printer := message.NewPrinter(language.English)

	switch timeRange {
	case Day:
		builder.WriteString(fmt.Sprintf("<b>Lịch sử giá %s ngày %s</b>", goldPrices[0].GoldName, t.Format("02/01/2006")))
	case Week:
		startOfDay := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())

		weekday := int(startOfDay.Weekday())
		if weekday == 0 {
			weekday = 7
		}

		startOfWeek := startOfDay.AddDate(0, 0, -(weekday - 1))
		endOfWeek := startOfWeek.AddDate(0, 0, 6)
		builder.WriteString(fmt.Sprintf("<b>Lịch sử giá %s tuần %02d - %02d/%02d/%d</b>", goldPrices[0].GoldName, startOfWeek.Day(), endOfWeek.Day(), startOfWeek.Month(), startOfWeek.Year()))

		formatPrice(&builder, printer, goldPrices[len(goldPrices)-1].Buy, goldPrices[0].Buy, "Biến động mua vào", true)
		formatPrice(&builder, printer, goldPrices[len(goldPrices)-1].Sell, goldPrices[0].Sell, "Biến động bán ra", true)

		builder.WriteString("\n\n<b>Chi tiết giá vàng từng ngày trong tuần:</b>")
	case Month:
		builder.WriteString(fmt.Sprintf("<b>Lịch sử giá %s tháng %s</b>", goldPrices[0].GoldName, t.Format("01/2006")))

		formatPrice(&builder, printer, goldPrices[len(goldPrices)-1].Buy, goldPrices[0].Buy, "Biến động mua vào", true)
		formatPrice(&builder, printer, goldPrices[len(goldPrices)-1].Sell, goldPrices[0].Sell, "Biến động bán ra", true)

		builder.WriteString("\n\n<b>Chi tiết giá vàng từng ngày trong tháng:</b>")
	case Year:
		builder.WriteString(fmt.Sprintf("<b>Lịch sử giá %s năm %s</b>", goldPrices[0].GoldName, t.Format("2006")))
		builder.WriteString(fmt.Sprintf("\n<i>Biến động giá vàng từ %s đến %s</i>",
			time.Date(t.Year(), 1, 1, 0, 0, 0, 0, t.Location()).Format("02/01/2006"),
			t.Format("02/01/2006")))

		formatPrice(&builder, printer, goldPrices[len(goldPrices)-1].Buy, goldPrices[0].Buy, "Mua vào", true)
		formatPrice(&builder, printer, goldPrices[len(goldPrices)-1].Sell, goldPrices[0].Sell, "Bán ra", true)

		builder.WriteString("\n\n<b>Chi tiết giá vàng từng tháng trong năm:</b>")
	}

	for i, goldPrice := range goldPrices {
		switch timeRange {
		case Day:
			if goldPrice.PriceDate != t.Format(time.DateOnly) {
				continue
			}
			formatPrice(&builder, printer, goldPrices[len(goldPrices)-1].Buy, goldPrice.Buy, "Mua vào", false)

			builder.WriteString(printer.Sprintf("\n<i> Thấp nhất trong ngày: %d</i>", goldPrice.LowBuy))
			builder.WriteString(printer.Sprintf("\n<i> Cao nhất trong ngày: %d</i>", goldPrice.HighBuy))

			builder.WriteString("\n")

			formatPrice(&builder, printer, goldPrices[len(goldPrices)-1].Sell, goldPrice.Sell, "Bán ra", false)

			builder.WriteString(printer.Sprintf("\n<i> Thấp nhất trong ngày: %d</i>", goldPrice.LowSell))
			builder.WriteString(printer.Sprintf("\n<i> Cao nhất trong ngày: %d</i>", goldPrice.HighSell))
		case Week, Month:
			priceDate, err := time.Parse(time.DateOnly, goldPrice.PriceDate)
			if err != nil {
				return nil, err
			}
			builder.WriteString(printer.Sprintf("\n<i> ✦ %s - Mua vào: %d - Bán ra: %d</i>", priceDate.Format("02/01/2006"), goldPrice.Buy, goldPrice.Sell))
		case Year:
			if i == len(goldPrices)-1 {
				continue
			}
			priceDate, err := time.Parse(time.DateOnly, goldPrice.PriceDate)
			if err != nil {
				return nil, err
			}
			builder.WriteString(printer.Sprintf("\n<i> ✦ Tháng %s - Mua vào: %d - Bán ra: %d</i>", priceDate.Format("01/2006"), goldPrice.Buy, goldPrice.Sell))
		}
	}

	builder.WriteString("\n\n<i>(Đơn vị tính: nghìn đồng/chỉ)</i>")

	timeRangeButtons := make([]InlineKeyboardButton, 0, len(TimeRanges))

	for _, tr := range TimeRanges {
		prefix := ""
		if tr == timeRange {
			prefix = "✦ "
		}

		timeRangeButtons = append(timeRangeButtons, InlineKeyboardButton{
			Text:         fmt.Sprintf("%s%s", prefix, TimeRangeLabels[tr]),
			CallbackData: fmt.Sprintf("%s%s%d%s%s", GoldPriceHistoryCode, Separator, goldID, Separator, tr),
		})
	}

	return &BotMessage{
		ParseMode: "HTML",
		Text:      builder.String(),
		ReplyMarkup: &ReplyMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				timeRangeButtons,
				{
					{
						Text:         "<< Quay lại",
						CallbackData: HistoryCode,
					},
				},
			},
		},
	}, nil
}

var FeedbackFunc = func(ctx *context.Context, chatID int, params ...string) (*BotMessage, error) {
	builder := strings.Builder{}
	builder.WriteString("<b>Gửi góp ý cải thiện Bot theo cú pháp</b>")
	builder.WriteString(fmt.Sprintf("\n<b><i><code>%s</code> 'nội dung'</i></b>", Feedback))
	builder.WriteString(fmt.Sprintf("\n\n<b><i>Ví dụ: <code>%s</code> Bot rất hữu ích!!!</i></b>", Feedback))

	return &BotMessage{
		ParseMode: "HTML",
		Text:      builder.String(),
	}, nil
}

var DonateFunc = func(ctx *context.Context, chatID int, params ...string) (*BotMessage, error) {
	return &BotMessage{
		ParseMode: "HTML",
		Text:      "<b>🌱 Cảm ơn bạn đã đồng hành cùng 🐶 Cậu Vàng!\n\n<i>Thả ♥️ hoặc donate qua STK TPBank: <code>77899230400</code> - Nguyen Van Bach</i></b>",
	}, nil
}

var InboxFunc = func(ctx *context.Context, chatID int, params ...string) (*BotMessage, error) {
	if len(params) != 1 {
		return nil, nil
	}

	text, err := board(ctx, params[0])
	if err != nil {
		return nil, err
	}

	if text == "" {
		return nil, nil
	}

	return &BotMessage{
		ParseMode: "HTML",
		Text:      text,
	}, nil
}

var fns = map[string]func(ctx *context.Context, chatID int, params ...string) (*BotMessage, error){
	IntroCode:            IntroFunc,
	LiveCode:             LiveFunc,
	NotifCode:            NotifFunc,
	VolatilityCode:       VolatilityFunc,
	SchedulesCode:        SchedulesFunc,
	ScheduleCode:         ScheduleFunc,
	SelectHourCode:       SelectHourFunc,
	SelectMinuteCode:     SelectMinuteFunc,
	SelectDaysOfWeekCode: SelectDaysOfWeekFunc,
	ConfirmScheduleCode:  ConfirmScheduleFunc,
	DeleteScheduleCode:   DeleteScheduleFunc,
	HistoryCode:          HistoryFunc,
	GoldPriceHistoryCode: GoldPriceHistoryFunc,
	FeedbackCode:         FeedbackFunc,
	DonateCode:           DonateFunc,
	InboxCode:            InboxFunc,
}

type GoldPrice struct {
	GoldID    int
	GoldName  string
	PriceDate string
	RefBuy    int
	Buy       int
	LowBuy    int
	HighBuy   int
	RefSell   int
	Sell      int
	LowSell   int
	HighSell  int
}

func fetchGoldPriceLatest(s *store.Store) ([]GoldPrice, error) {
	var goldPrices []GoldPrice

	if err := s.SQL.QueryRows(
		`select gold_id, name, price_date, ref_buy, buy, low_buy, high_buy, ref_sell, sell, low_sell, high_sell
		from gold_price_latest gpl
		join golds g on gpl.gold_id = g.id`,
		func(rows *sql.Rows) error {
			goldPrice := GoldPrice{}
			if err := rows.Scan(
				&goldPrice.GoldID,
				&goldPrice.GoldName,
				&goldPrice.PriceDate,
				&goldPrice.RefBuy,
				&goldPrice.Buy,
				&goldPrice.LowBuy,
				&goldPrice.HighBuy,
				&goldPrice.RefSell,
				&goldPrice.Sell,
				&goldPrice.LowSell,
				&goldPrice.HighSell,
			); err != nil {
				return err
			}

			goldPrices = append(goldPrices, goldPrice)

			return nil
		},
	); err != nil {
		return nil, err
	}

	return goldPrices, nil
}

func isVolatilityNotifEnabled(s *store.Store, chatID int) (bool, error) {
	var enabled bool

	if err := s.SQL.QueryRow(
		`select enabled from volatility_notif_subcriptions where chat_id = ?`,
		func(row *sql.Row) error {
			return row.Scan(&enabled)
		},
		chatID,
	); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	}

	return enabled, nil
}

func saveVolatilityNotifSubcription(s *store.Store, chatID int, enabled bool) error {
	return s.SQL.WithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`insert into volatility_notif_subcriptions (chat_id, enabled)
			values (?, ?)
			on conflict (chat_id) do update
			set enabled = excluded.enabled`,
			chatID, enabled,
		)
		return err
	})
}

func fetchScheduleNotifSubcriptions(s *store.Store, chatID int) ([]store.ScheduleNotifSubcription, error) {
	var subcriptions []store.ScheduleNotifSubcription

	if err := s.SQL.QueryRows(
		`select chat_id, hour, minute, days_of_week, enabled from schedule_notif_subcriptions where chat_id = ?`,
		func(rows *sql.Rows) error {
			var subcription store.ScheduleNotifSubcription

			if scanErr := rows.Scan(&subcription.ChatID, &subcription.Hour, &subcription.Minute, &subcription.DaysOfWeek, &subcription.Enabled); scanErr != nil {
				return scanErr
			}

			subcriptions = append(subcriptions, subcription)

			return nil
		},
		chatID,
	); err != nil {
		return nil, err
	}

	return subcriptions, nil
}

func findScheduleNotifSubcription(s *store.Store, chatID, hour, minute, daysOfWeek int) (*store.ScheduleNotifSubcription, error) {
	subcription := &store.ScheduleNotifSubcription{}

	if err := s.SQL.QueryRow(
		`select chat_id, hour, minute, days_of_week, enabled from schedule_notif_subcriptions where chat_id = ? and hour = ? and minute = ? and days_of_week = ?`,
		func(row *sql.Row) error {
			return row.Scan(&subcription.ChatID, &subcription.Hour, &subcription.Minute, &subcription.DaysOfWeek, &subcription.Enabled)
		},
		chatID, hour, minute, daysOfWeek,
	); err != nil {
		return nil, err
	}

	return subcription, nil
}

func toggleScheduleNotifSubcriptionEnabled(s *store.Store, subcription *store.ScheduleNotifSubcription) error {
	return s.SQL.WithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`update schedule_notif_subcriptions set enabled = ? where chat_id = ? and hour = ? and minute = ? and days_of_week = ?`,
			subcription.Enabled, subcription.ChatID, subcription.Hour, subcription.Minute, subcription.DaysOfWeek,
		)
		return err
	})
}

func insertScheduleNotifSubcription(s *store.Store, subcription *store.ScheduleNotifSubcription) error {
	return s.SQL.WithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`insert into schedule_notif_subcriptions (chat_id, hour, minute, days_of_week)
			values (?, ?, ?, ?)`,
			subcription.ChatID, subcription.Hour, subcription.Minute, subcription.DaysOfWeek,
		)
		return err
	})
}

func saveScheduleNotifSubcription(s *store.Store, unsavedSubcription *store.ScheduleNotifSubcription, savedSubcription *store.ScheduleNotifSubcription) error {
	return s.SQL.WithTx(func(tx *sql.Tx) error {
		result, err := tx.Exec(
			`update schedule_notif_subcriptions set hour = ?, minute = ?, days_of_week = ?, latest_notif = 0 where chat_id = ? and hour = ? and minute = ? and days_of_week = ?`,
			unsavedSubcription.Hour, unsavedSubcription.Minute, unsavedSubcription.DaysOfWeek, savedSubcription.ChatID, savedSubcription.Hour, savedSubcription.Minute, savedSubcription.DaysOfWeek,
		)
		if err != nil {
			return err
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return err
		}

		if rowsAffected == 0 {
			if _, err = tx.Exec(
				`insert into schedule_notif_subcriptions (chat_id, hour, minute, days_of_week)
			values (?, ?, ?, ?)`,
				unsavedSubcription.ChatID, unsavedSubcription.Hour, unsavedSubcription.Minute, unsavedSubcription.DaysOfWeek,
			); err != nil {
				return err
			}
		}

		return nil
	})
}

func deleteScheduleNotifSubcription(s *store.Store, subcription *store.ScheduleNotifSubcription) error {
	return s.SQL.WithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`delete from schedule_notif_subcriptions where chat_id = ? and hour = ? and minute = ? and days_of_week = ?`,
			subcription.ChatID, subcription.Hour, subcription.Minute, subcription.DaysOfWeek,
		)
		return err
	})
}

func fetchGolds(s *store.Store) ([]store.Gold, error) {
	var golds []store.Gold

	if err := s.SQL.QueryRows(
		`select id, name, mask from golds`,
		func(rows *sql.Rows) error {
			gold := store.Gold{}
			if err := rows.Scan(&gold.ID, &gold.Name, &gold.Mask); err != nil {
				return err
			}

			golds = append(golds, gold)

			return nil
		},
	); err != nil {
		return nil, err
	}

	return golds, nil
}

func findGoldPriceHistory(s *store.Store, goldID int, priceDate string) (*GoldPriceHistory, error) {
	goldPrice := &GoldPriceHistory{}

	if err := s.SQL.QueryRow(
		`select gold_id, name, price_date, buy, low_buy, high_buy, sell, low_sell, high_sell 
		from gold_price_history gph 
    	join golds g on gph.gold_id = g.id
		where gold_id = ? and price_date = ?`,
		func(row *sql.Row) error {
			return row.Scan(
				&goldPrice.GoldID,
				&goldPrice.GoldName,
				&goldPrice.PriceDate,
				&goldPrice.Buy,
				&goldPrice.LowBuy,
				&goldPrice.HighBuy,
				&goldPrice.Sell,
				&goldPrice.LowSell,
				&goldPrice.HighSell,
			)
		},
		goldID, priceDate,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return goldPrice, nil
}

type GoldPriceHistory struct {
	GoldID    int
	GoldName  string
	PriceDate string
	Buy       int
	LowBuy    int
	HighBuy   int
	Sell      int
	LowSell   int
	HighSell  int
}

func fetchGoldPriceHistory(s *store.Store, goldID int, timeRange string, t time.Time) ([]GoldPriceHistory, error) {
	var goldPrices []GoldPriceHistory

	params := []any{goldID}

	builder := strings.Builder{}

	builder.WriteString("select gold_id, name, price_date, buy, low_buy, high_buy, sell, low_sell, high_sell")
	builder.WriteString(" from gold_price_history gph")
	builder.WriteString(" join golds g on gph.gold_id = g.id")
	builder.WriteString(" where gph.gold_id = ?")

	switch timeRange {
	case Day:
		builder.WriteString(" and gph.price_date between ? and ?")

		params = append(params, t.AddDate(0, 0, -1).Format(time.DateOnly), t.Format(time.DateOnly))
	case Week:
		builder.WriteString(" and gph.price_date between ? and ?")

		startOfDay := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())

		weekday := int(startOfDay.Weekday())
		if weekday == 0 {
			weekday = 7
		}

		startOfWeek := startOfDay.AddDate(0, 0, -(weekday - 1))

		params = append(params, startOfWeek.Format(time.DateOnly), t.Format(time.DateOnly))
	case Month:
		builder.WriteString(" and gph.price_date between ? and ?")

		startOfMonth := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())

		params = append(params, startOfMonth.Format(time.DateOnly), t.Format(time.DateOnly))
	case Year:
		currMonth := t.Month()

		endOfmonths := make([]string, 0, currMonth)

		for i := time.January; i <= currMonth; i++ {
			if i == currMonth {
				endOfmonths = append(endOfmonths, t.Format(time.DateOnly))
				continue
			}

			endOfMonth := time.Date(t.Year(), i, 1, 0, 0, 0, 0, t.Location()).AddDate(0, 1, -1)

			endOfmonths = append(endOfmonths, endOfMonth.Format(time.DateOnly))
		}

		placeholders := make([]string, 0, len(endOfmonths))
		for _, endOfmonth := range endOfmonths {
			placeholders = append(placeholders, "?")
			params = append(params, endOfmonth)
		}
		placeholders = append(placeholders, "?")
		params = append(params, time.Date(t.Year(), 1, 1, 0, 0, 0, 0, t.Location()).Format(time.DateOnly))
		builder.WriteString(fmt.Sprintf(" and gph.price_date in (%s)", strings.Join(placeholders, ",")))
	}

	builder.WriteString(" order by gph.price_date desc")

	if err := s.SQL.QueryRows(
		builder.String(),
		func(rows *sql.Rows) error {
			goldPrice := GoldPriceHistory{}

			if err := rows.Scan(
				&goldPrice.GoldID,
				&goldPrice.GoldName,
				&goldPrice.PriceDate,
				&goldPrice.Buy,
				&goldPrice.LowBuy,
				&goldPrice.HighBuy,
				&goldPrice.Sell,
				&goldPrice.LowSell,
				&goldPrice.HighSell,
			); err != nil {
				return err
			}

			goldPrices = append(goldPrices, goldPrice)

			return nil
		},
		params...,
	); err != nil {
		return nil, err
	}

	return goldPrices, nil
}

func handleParams(n int, params ...string) (saved, unsaved int, isNew bool, index int, err error) {
	if n != len(params) {
		err = fmt.Errorf("invalid number of parameters")
		return
	}

	saved, err = strconv.Atoi(params[0])
	if err != nil {
		return
	}

	if n > 1 {
		unsaved, err = strconv.Atoi(params[1])
		if err != nil {
			return
		}
	}

	if n >= 3 {
		isNew, err = strconv.ParseBool(params[2])
		if err != nil {
			return
		}
	}

	if n >= 4 {
		index, err = strconv.Atoi(params[3])
	}

	return
}

func page(code string, saved, unsaved, index, n int, isNew bool) []InlineKeyboardButton {
	hour, minute, daysOfWeek := decodeSchedule(unsaved)

	var buttons []InlineKeyboardButton

	buttons = append(buttons, InlineKeyboardButton{
		Text:         "←",
		CallbackData: fmt.Sprintf("%s%s%d%s%d%s%s%s%d", code, Separator, saved, Separator, unsaved, Separator, strconv.FormatBool(isNew), Separator, (index-1+n)%n),
	})

	for i := index * 6; i < index*6+6; i++ {
		prefix := ""
		if (code == SelectHourCode && i == hour) || (code == SelectMinuteCode && i == minute) {
			prefix = "✦ "
		}

		text := fmt.Sprintf("%s%02d", prefix, i)

		mask := 0
		if code == SelectHourCode {
			mask = encodeSchedule(i, minute, daysOfWeek)
		} else if code == SelectMinuteCode {
			mask = encodeSchedule(hour, i, daysOfWeek)
		}

		buttons = append(buttons, InlineKeyboardButton{
			Text:         text,
			CallbackData: fmt.Sprintf("%s%s%d%s%d%s%s%s%d", code, Separator, saved, Separator, mask, Separator, strconv.FormatBool(isNew), Separator, index),
		})
	}

	buttons = append(buttons, InlineKeyboardButton{
		Text:         "→",
		CallbackData: fmt.Sprintf("%s%s%d%s%d%s%s%s%d", code, Separator, saved, Separator, unsaved, Separator, strconv.FormatBool(isNew), Separator, (index+1)%n),
	})

	return buttons
}

func board(ctx *context.Context, title string) (string, error) {
	goldPrices, err := fetchGoldPriceLatest(ctx.Store)
	if err != nil {
		return "", err
	}

	if len(goldPrices) == 0 {
		return "", nil
	}

	now := time.Now().In(ctx.Config.TimeLocation).Format(time.DateOnly)

	builder := strings.Builder{}

	builder.WriteString(title)

	printer := message.NewPrinter(language.English)

	var syncNewDay bool
	for _, goldPrice := range goldPrices {
		if goldPrice.PriceDate != now {
			goldPrice.RefBuy = goldPrice.Buy
			goldPrice.LowBuy = goldPrice.Buy
			goldPrice.HighBuy = goldPrice.Buy
			goldPrice.RefSell = goldPrice.Sell
			goldPrice.LowSell = goldPrice.Sell
			goldPrice.HighSell = goldPrice.Sell
			goldPrice.PriceDate = now

			syncNewDay = true
		}

		builder.WriteString(fmt.Sprintf("\n\n<b>%s</b>", goldPrice.GoldName))

		formatPrice(&builder, printer, goldPrice.RefBuy, goldPrice.Buy, "Mua vào", false)
		formatPrice(&builder, printer, goldPrice.RefSell, goldPrice.Sell, "Bán ra", false)
	}

	builder.WriteString("\n\n<i>(Đơn vị tính: nghìn đồng/chỉ)</i>")

	if syncNewDay {
		if err = HandleNewDay(ctx); err != nil {
			log.Error().Err(err).Send()
		}
	}

	return builder.String(), nil
}

func formatPrice(builder *strings.Builder, printer *message.Printer, refPrice, price int, title string, isTimeRange bool) {
	if isTimeRange {
		builder.WriteString(printer.Sprintf("\n<i> ✦ %s: </i>", title))
	} else {
		builder.WriteString(printer.Sprintf("\n<i> ✦ %s: %d</i>", title, price))
	}

	deltaBuy := price - refPrice
	percentBuy := float64(deltaBuy) / float64(refPrice) * 100

	if isTimeRange {
		builder.WriteString(" <i>")
	} else {
		builder.WriteString(" <i>(")
	}

	if deltaBuy > 0 {
		builder.WriteString("▲ ")
	} else if deltaBuy < 0 {
		builder.WriteString("▼ ")
	}

	if deltaBuy != 0 {
		builder.WriteString(printer.Sprintf("%+d | %+0.2f%%", deltaBuy, percentBuy))
	} else {
		builder.WriteString(printer.Sprintf("%d | %0.2f%%", deltaBuy, percentBuy))
	}

	if isTimeRange {
		builder.WriteString("</i>")
	} else {
		builder.WriteString(")</i>")
	}
}

var DaysOfWeek = map[int]string{
	1:  "T2",
	2:  "T3",
	4:  "T4",
	8:  "T5",
	16: "T6",
	32: "T7",
	64: "CN",
}

var DayGroups = map[int]string{
	31:  "Ngày thường",
	96:  "Cuối tuần",
	127: "Hàng ngày",
}

func formatDaysOfWeek(mask int) string {
	if dayGroup, exist := DayGroups[mask]; exist {
		return dayGroup
	}

	if day, exist := DaysOfWeek[mask]; exist {
		return day
	}

	var days []string
	for i := 0; i < 7; i++ {
		if mask&(1<<i) != 0 {
			days = append(days, DaysOfWeek[1<<i])
		}
	}

	return strings.Join(days, ", ")
}

func encodeSchedule(hour, minute, daysOfWeek int) int {
	return hour<<13 | minute<<7 | daysOfWeek
}

func decodeSchedule(mask int) (hour, minute int, daysOfWeek int) {
	hour = (mask >> 13) & 0x1f
	minute = (mask >> 7) & 0x3f
	daysOfWeek = mask & 0x7f
	return
}

const (
	Day   = "d"
	Week  = "w"
	Month = "m"
	Year  = "y"
)

var TimeRanges = []string{
	Day,
	Week,
	Month,
	Year,
}

var TimeRangeLabels = map[string]string{
	Day:   "Ngày",
	Week:  "Tuần",
	Month: "Tháng",
	Year:  "Năm",
}

func (t *Telegram) sendFeedback(feedback string) {
	reqBody, err := json.Marshal(SendMessageRequest{
		ChatID:    t.Ctx.Config.Bot.FeedbackReceiverChatID,
		ParseMode: "HTML",
		Text:      feedback,
	})
	if err != nil {
		log.Error().Err(err).Send()
		return
	}

	_, err = t.Ctx.TelegramHTTPClient.Do(
		&request.Request{
			Method: http.MethodPost,
			URL:    t.Ctx.Config.Bot.FeedbackBotURL + "/sendMessage",
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: reqBody,
		},
	)
	if err != nil {
		log.Error().Err(err).Send()
	}
}

func HandleNewDay(ctx *context.Context) error {
	now := time.Now().In(ctx.Config.TimeLocation).Format(time.DateOnly)

	return ctx.Store.SQL.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`update gold_price_latest
			set price_date = ?,
			    ref_buy = buy,
				low_buy = buy,
				high_buy = buy,
				ref_sell = sell,
				low_sell = sell,
				high_sell = sell
			where price_date < ?`,
			now,
		); err != nil {
			return err
		}

		if _, err := tx.Exec(
			`insert or ignore into gold_price_history
    		(gold_id, price_date, buy, low_buy, high_buy, sell, low_sell, high_sell)
			select gold_id, price_date, buy, low_buy, high_buy, buy, low_sell, high_sell
			from gold_price_latest
			where price_date = ?`,
			now,
		); err != nil {
			return err
		}

		return nil
	})
}
