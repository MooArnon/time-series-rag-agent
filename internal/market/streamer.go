package market

import (
	// "encoding/json"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	// "github.com/gorilla/websocket"
)

type KLineEvent struct {
	E              int64     `json:"E"`
	EventTimestamp int64     `json:"E"`
	Symbol         string    `json:"s"`
	KLine          KLineData `json:"k"`
}

type KLineData struct {
	StartTime int64  `json:"t"`
	EndTime   int64  `json:"T"`
	Symbol    string `json:"s"`
	Interval  string `json:"i"`

	OpenPrice  json.Number `json:"o"`
	ClosePrice json.Number `json:"c"`
	HighPrice  json.Number `json:"h"`
	LowPrice   json.Number `json:"l"`
	Volume     json.Number `json:"v"`

	IsClose bool `json:"x"`
}

type KLineStreamer struct {
	Symbol   string
	Interval string
	DataChan chan KLineEvent
	wsUrl    string
	Logger   *slog.Logger
}

func NewKLineStreamer(
	symbol string,
	interval string,
	logger *slog.Logger,
) *KLineStreamer {
	lowerSymbol := strings.ToLower(symbol)

	url := fmt.Sprintf("wss://fstream.binance.com/ws/%s@kline_%s", lowerSymbol, interval)

	return &KLineStreamer{
		Symbol:   symbol,
		Interval: interval,
		DataChan: make(chan KLineEvent, 100),
		Logger:   logger,
		wsUrl:    url,
	}
}

func (s *KLineStreamer) Start() {
	s.Logger.Info("Starting KLineStreamer")
	defer close(s.DataChan)

	for {
		s.Logger.Info("Connecting to Binance stream", "url", s.wsUrl)

		// Connect
		conn, _, err := websocket.DefaultDialer.Dial(s.wsUrl, nil)
		if err != nil {
			s.Logger.Error("Connection Failed", "error", err)

			time.Sleep(5 * time.Second)
			continue // Go to top of loop
		}

		s.Logger.Info("Connected to Binance")

		// Reading loop til error
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				s.Logger.Error("Reading error", "error", err)
				break // Break this inner loop to reconnect
			}

			// Parse data
			var event KLineEvent
			if err := json.Unmarshal(message, &event); err != nil {
				s.Logger.Error("Json Parse error", "error", err)
				continue
			}

			s.DataChan <- event
		}
		conn.Close()
		time.Sleep(1 * time.Second)
	}
}
