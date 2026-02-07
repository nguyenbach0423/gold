package store

type Gold struct {
	ID   int
	Name string
	Mask int
}

type GoldPriceLatest struct {
	GoldID    int
	PriceDate string
	Buy       int
	LowBuy    int
	HighBuy   int
	Sell      int
	LowSell   int
	HighSell  int
}

type GoldPriceHistory struct {
	GoldID    int
	PriceDate string
	Buy       int
	LowBuy    int
	HighBuy   int
	Sell      int
	LowSell   int
	HighSell  int
}

type Chat struct {
	ID        int
	Type      string
	Username  string
	FirstName string
	LastName  string
}

type VolatilityNotifSubcription struct {
	ChatID  int
	Enabled bool
}

type ScheduleNotifSubcription struct {
	ChatID      int
	Hour        int
	Minute      int
	DaysOfWeek  int
	Enabled     bool
	LatestNotif int
}
