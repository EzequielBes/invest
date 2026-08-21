// evaluate-outcomes records due subscription intent outcomes from local candles.
package main

import (
	"context"
	"log"
	"os"

	"strategist/runner"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	count, err := runner.EvaluateOutcomesWithDSN(context.Background(), dsn)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("recorded %d intent outcomes", count)
}
