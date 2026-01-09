package main

import (
	"fmt"
	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/market"
	logger "time-series-rag-agent/pkg"
)

func main() {
	// Configuration
	appConfig := config.LoadConfig()

	// Logger object
	log := logger.SetupLogger()
	streamer := market.NewKLineStreamer(
		"adausdt",
		"1m",
		log,
	)

	log.Info(fmt.Sprintf("Running logic at database %s", appConfig.Database.DBName))

	go streamer.Start()

	for event := range streamer.DataChan {
		k := event.KLine

		if k.IsClose {
			fmt.Printf(
				"Candle was closed [%s]: OpenPrice=%s | ClosePrice=%s | Volume=%s\n",
				k.Interval, k.OpenPrice, k.ClosePrice, k.Volume,
			)
		} else {
			fmt.Printf("Price updating: %s\r", k.ClosePrice)
		}
	}
}
