package middleware

import (
	"bytes"
	"io"
	"time"

	"github.com/nguyenbach0423/httpx/server"
	"github.com/rs/zerolog/log"
)

func Logger() server.MiddlewareFunc {
	return func(next server.HandlerFunc) server.HandlerFunc {
		return func(c *server.Context) {
			start := time.Now()

			event := log.Info().
				Str("method", c.Request.Method).
				Str("path", c.Request.URL.Path)

			requestBody, err := io.ReadAll(io.LimitReader(c.Request.Body, 10<<20))
			if err != nil {
				log.Error().Err(err).Send()
			} else {
				c.Request.Body = io.NopCloser(bytes.NewReader(requestBody))
			}

			if len(requestBody) > 0 {
				event.RawJSON("request_body", requestBody)
			}

			if c.Request.URL.RawQuery != "" {
				event.Interface("query_params", c.Request.URL.Query())
			}

			next(c)

			if c.Error != nil {
				event.Err(c.Error)
			}

			event.Int("status", c.Response.Status).
				Int64("latency", time.Since(start).Milliseconds())

			if len(c.Response.Body) > 0 {
				event.RawJSON("response_body", c.Response.Body)
			}

			event.Send()
		}
	}
}
