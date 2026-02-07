package handler

import (
	"encoding/json"
	"net/http"

	"github.com/nguyenbach0423/gold/telegram"
	"github.com/nguyenbach0423/httpx/server"
	"github.com/nguyenbach0423/workerpool"
)

func BotHandler(c *server.Context) {
	bot := c.Params["bot"].(*telegram.Telegram)
	wp := c.Params["workerpool"].(*workerpool.WorkerPool)

	var update *telegram.Update
	if err := json.NewDecoder(c.Request.Body).Decode(&update); err != nil {
		c.Abort(http.StatusBadRequest)
		return
	}

	bot.HandleUpdate(wp, update)

	c.Abort(http.StatusOK)
}
