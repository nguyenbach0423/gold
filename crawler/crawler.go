package crawler

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/nguyenbach0423/gold/context"
	"github.com/nguyenbach0423/gold/notif"
	"github.com/nguyenbach0423/gold/telegram"
	"github.com/nguyenbach0423/httpx/client/request"
	"github.com/nguyenbach0423/workerpool"
	"github.com/rs/zerolog/log"
)

type Crawler struct {
	Ctx *context.Context
}

func (cr *Crawler) Run(wp *workerpool.WorkerPool, bot *telegram.Telegram) {
	now := time.Now().In(cr.Ctx.Config.TimeLocation)
	if now.Weekday() == time.Sunday {
		return
	}

	cr.runTasks(
		wp, bot,
		func() (bool, error) {
			resp, err := cr.Ctx.CrawlerHTTPClient.Do(
				&request.Request{
					Method: http.MethodGet,
					URL:    "https://sjc.com.vn/GoldPrice/Services/PriceService.ashx",
				},
			)
			if err != nil {
				return false, err
			}

			if resp.Status != http.StatusOK {
				return false, nil
			}

			var body struct {
				PriceDate  string `json:"latestDate"`
				GoldPrices []struct {
					GoldID int    `json:"Id"`
					Buy    string `json:"Buy"`
					Sell   string `json:"Sell"`
				} `json:"data"`
			}

			if err = json.Unmarshal(resp.Body, &body); err != nil {
				return false, err
			}

			priceDate, err := time.Parse("15:04 02/01/2006", body.PriceDate)

			var res bool
			var foundRows int

			for _, goldPrice := range body.GoldPrices {
				switch goldPrice.GoldID {
				case 1:
					goldPrice.GoldID = 1
				case 49:
					goldPrice.GoldID = 2
				default:
					continue
				}

				foundRows++

				var buy int
				buy, err = cr.parsePrice(goldPrice.Buy, 10)
				if err != nil {
					return false, err
				}

				var sell int
				sell, err = cr.parsePrice(goldPrice.Sell, 10)
				if err != nil {
					return false, err
				}

				err = cr.saveGoldPrice(priceDate.Format(time.DateOnly), goldPrice.GoldID, buy, sell)

				if err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						if foundRows == 2 {
							break
						}
						continue
					}
					return false, err
				}

				res = true

				if foundRows == 2 {
					break
				}
			}

			return res, nil
		},
		func() (bool, error) {
			resp, err := cr.Ctx.CrawlerHTTPClient.Do(
				&request.Request{
					Method: http.MethodGet,
					URL:    "https://edge-api.pnj.io/ecom-frontend/v1/get-gold-price",
					QueryParams: map[string][]string{
						"zone": {"11"},
					},
				},
			)
			if err != nil {
				return false, err
			}

			if resp.Status != http.StatusOK {
				return false, nil
			}

			var body struct {
				PriceDate  string `json:"updateDate"`
				GoldPrices []struct {
					GoldCode string `json:"masp"`
					Buy      int    `json:"giamua"`
					Sell     int    `json:"giaban"`
				} `json:"data"`
			}

			if err = json.Unmarshal(resp.Body, &body); err != nil {
				return false, err
			}

			priceDate, err := time.Parse("02/01/2006 15:04:05", body.PriceDate)
			if err != nil {
				return false, err
			}

			for _, goldPrice := range body.GoldPrices {
				if goldPrice.GoldCode == "N24K" {
					err = cr.saveGoldPrice(priceDate.Format(time.DateOnly), 3, goldPrice.Buy, goldPrice.Sell)
					if err != nil {
						if errors.Is(err, sql.ErrNoRows) {
							return false, nil
						}
						return false, err
					}
					return true, nil
				}
			}

			return false, nil
		},
		func() (bool, error) {
			return cr.fromHTML(
				"https://giavang.doji.vn",
				"div.ant-home-price table.goldprice-view",
				"span.update-time",
				`\d{2}:\d{2}\s\d{2}/\d{2}/\d{4}`,
				"15:04 02/01/2006",
				4,
				"nhẫn tròn 9999 hưng thịnh vượng(nghìn/chỉ)",
				0,
				1,
				2,
				1,
			)
		},
		func() (bool, error) {
			resp, err := cr.Ctx.CrawlerHTTPClient.Do(
				&request.Request{
					Method: http.MethodGet,
					URL:    "http://api.btmc.vn/api/BTMCAPI/getpricebtmc",
					QueryParams: map[string][]string{
						"key": {"3kd8ub1llcg9t45hnoh8hmn7t5kc2v"},
					},
				},
			)
			if err != nil {
				return false, err
			}

			if resp.Status != http.StatusOK {
				return false, nil
			}

			var body struct {
				DataList struct {
					GoldPrices []map[string]string `json:"Data"`
				} `json:"DataList"`
			}

			if err = json.Unmarshal(resp.Body, &body); err != nil {
				return false, err
			}

			for i := 1; i <= len(body.DataList.GoldPrices); i++ {
				if strings.ToLower(strings.TrimSpace(body.DataList.GoldPrices[i-1][fmt.Sprintf("@n_%d", i)])) != "nhẫn tròn trơn (vàng rồng thăng long)" {
					var priceDate time.Time
					if priceDate, err = time.Parse("02/01/2006 15:04", strings.TrimSpace(body.DataList.GoldPrices[i-1][fmt.Sprintf("@d_%d", i)])); err != nil {
						return false, err
					}

					var buy int
					if buy, err = cr.parsePrice(strings.TrimSpace(body.DataList.GoldPrices[i-1][fmt.Sprintf("@pb_%d", i)]), 1000); err != nil {
						return false, err
					}

					var sell int
					if sell, err = cr.parsePrice(strings.TrimSpace(body.DataList.GoldPrices[i-1][fmt.Sprintf("@ps_%d", i)]), 1000); err != nil {
						return false, err
					}

					if err = cr.saveGoldPrice(priceDate.Format(time.DateOnly), 5, buy, sell); err != nil {
						if errors.Is(err, sql.ErrNoRows) {
							return false, nil
						}
						return false, err
					}
					
					return true, nil
				}
			}
			
			return false, nil
		},
		func() (bool, error) {
			return cr.fromHTML(
				"https://baotinmanhhai.vn/gia-vang-hom-nay",
				"table.gold-table-content",
				"p.note",
				`\d{2}:\d{2}\s\d{2}/\d{2}/\d{4}`,
				"15:04 02/01/2006",
				6,
				"nhẫn tròn ép vỉ (kim gia bảo ) 24k (999.9)",
				0,
				1,
				2,
				1000,
			)
		},
		func() (bool, error) {
			return cr.fromHTML(
				"https://phuquygroup.vn",
				"table",
				"p.update-time",
				`\d{2}:\d{2}\s\d{2}/\d{2}/\d{4}`,
				"15:04 02/01/2006",
				7,
				"nhẫn tròn phú quý 999.9",
				0,
				1,
				2,
				1000,
			)
		},
	)
}

func (cr *Crawler) runTasks(wp *workerpool.WorkerPool, bot *telegram.Telegram, tasks ...func() (bool, error)) {
	if len(tasks) == 0 {
		return
	}

	var wg sync.WaitGroup
	resCh := make(chan bool, len(tasks))

	wg.Add(len(tasks))

	for _, task := range tasks {
		go func(fn func() (bool, error)) {
			defer wg.Done()

			res, err := fn()
			if err != nil {
				log.Error().Err(err).Send()
				return
			}

			resCh <- res
		}(task)
	}

	go func() {
		wg.Wait()
		close(resCh)
	}()

	results := make([]bool, 0, len(tasks))
	for res := range resCh {
		results = append(results, res)
	}

	for _, res := range results {
		if res {
			wp.Submit(func() {
				notif.SendVolatilityNotif(cr.Ctx, wp, bot)
			})

			break
		}
	}
}

func (cr *Crawler) fromHTML(url, tableSelector, priceDateSelector, reg, layout string, goldID int, goldName string, nameIdx, buyIdx, sellIdx, divisor int) (bool, error) {
	resp, err := cr.Ctx.CrawlerHTTPClient.Do(
		&request.Request{
			Method: http.MethodGet,
			URL:    url,
		},
	)
	if err != nil {
		return false, err
	}

	if resp.Status != http.StatusOK {
		return false, nil
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return false, err
	}

	priceDate, err := time.Parse(layout, regexp.MustCompile(reg).FindString(doc.Find(priceDateSelector).First().Text()))
	if err != nil {
		return false, err
	}

	var found bool

	tableSelector = fmt.Sprintf("%s tbody tr", tableSelector)

	doc.Find(tableSelector).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if goldName == strings.ToLower(strings.TrimSpace(s.Find("td").Eq(nameIdx).Text())) {
			found = true

			var buy int
			buy, err = cr.parsePrice(s.Find("td").Eq(buyIdx).Text(), divisor)
			if err != nil {
				return false
			}

			var sell int
			sell, err = cr.parsePrice(s.Find("td").Eq(sellIdx).Text(), divisor)
			if err != nil {
				return false
			}

			err = cr.saveGoldPrice(priceDate.Format(time.DateOnly), goldID, buy, sell)

			return false
		}

		return true
	})

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}

		return false, err
	}

	return found, nil
}

func (cr *Crawler) parsePrice(s string, divisor int) (int, error) {
	if s == "" {
		return 0, nil
	}

	s = strings.TrimSpace(s)

	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, ",", "")

	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}

	return n / divisor, nil
}

func (cr *Crawler) saveGoldPrice(priceDate string, goldID, buy, sell int) error {
	return cr.Ctx.Store.SQL.WithTx(func(tx *sql.Tx) error {
		if err := tx.QueryRow(
			`insert into gold_price_latest (gold_id, price_date, ref_buy, buy, low_buy, high_buy, ref_sell, sell, low_sell, high_sell)
			values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			on conflict (gold_id) do update
			set price_date = excluded.price_date,
			    ref_buy = case when price_date = excluded.price_date then ref_buy else buy end,
		    	buy = excluded.buy,
				low_buy = case when price_date = excluded.price_date then min(low_buy,excluded.buy) else excluded.buy end,
				high_buy = case when price_date = excluded.price_date then max(high_buy, excluded.buy) else excluded.buy end,
				ref_sell = case when price_date = excluded.price_date then ref_sell else sell end, 
				sell = excluded.sell,
				low_sell = case when price_date = excluded.price_date then min(low_sell, excluded.sell) else excluded.sell end,
				high_sell = case when price_date = excluded.price_date then max(high_sell, excluded.sell) else excluded.sell end
			where
		    	excluded.price_date > price_date
				or excluded.buy != buy
				or excluded.sell != sell
			returning gold_id`,
			goldID, priceDate, buy, buy, buy, buy, sell, sell, sell, sell,
		).Scan(&goldID); err != nil {
			return err
		}

		if _, err := tx.Exec(
			`insert into gold_price_history (gold_id, price_date, buy, low_buy, high_buy, sell, low_sell, high_sell)
			values (?, ?, ?, ?, ?, ?, ?, ?)
			on conflict (gold_id, price_date) do update
			set buy = excluded.buy,
			    low_buy = min(low_buy,excluded.buy),
				high_buy = max(high_buy, excluded.buy),
				sell = excluded.sell,
				low_sell = min(low_sell, excluded.sell),
				high_sell = max(high_sell, excluded.sell)
			where
				excluded.buy != buy
				or excluded.sell != sell`,
			goldID, priceDate, buy, buy, buy, sell, sell, sell,
		); err != nil {
			return err
		}

		return nil
	})
}
