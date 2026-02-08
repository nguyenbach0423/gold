package telegram

type Update struct {
	ID            int                `json:"update_id"`
	Message       *Message           `json:"message,omitempty"`
	CallbackQuery *CallbackQuery     `json:"callback_query,omitempty"`
	MyChatMember  *ChatMemberUpdated `json:"my_chat_member,omitempty"`
}

type ChatMemberUpdated struct {
	Chat Chat `json:"chat"`
}

type Message struct {
	ID   int    `json:"message_id"`
	Chat Chat   `json:"chat"`
	Text string `json:"text,omitempty"`
}

type Chat struct {
	ID        int    `json:"id"`
	Type      string `json:"type"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Title     string `json:"title,omitempty"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data,omitempty"`
}

type SetWebhookRequest struct {
	URL string `json:"url"`
}

type SetMyCommandsRequest struct {
	Commands []BotCommand `json:"commands"`
}

type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

type SendMessageRequest struct {
	ChatID      int          `json:"chat_id"`
	ParseMode   string       `json:"parse_mode,omitempty"`
	Text        string       `json:"text"`
	ReplyMarkup *ReplyMarkup `json:"reply_markup,omitempty"`
}

type AnswerCallbackQueryRequest struct {
	CallbackQueryID string `json:"callback_query_id"`
}

type EditMessageTextRequest struct {
	ChatID      int          `json:"chat_id"`
	MessageID   int          `json:"message_id"`
	ParseMode   string       `json:"parse_mode,omitempty"`
	Text        string       `json:"text"`
	ReplyMarkup *ReplyMarkup `json:"reply_markup,omitempty"`
}

type ReplyMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard,omitempty"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

type BotMessage struct {
	ParseMode   string       `json:"parse_mode,omitempty"`
	Text        string       `json:"text"`
	ReplyMarkup *ReplyMarkup `json:"reply_markup,omitempty"`
}

type SendPhotoRequest struct {
	ChatID    int    `json:"chat_id"`
	ParseMode string `json:"parse_mode,omitempty"`
	Caption   string `json:"caption,omitempty"`
	Filename  string `json:"filename"`
	Photo     []byte `json:"photo"`
}
