// mcp/cmd/mcp-server/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mcp/internal/storage"
	"mcp/internal/tools"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect storage: %w", err)
	}
	defer store.Close()

	server := mcp.NewServer(&mcp.Implementation{Name: "investment-platform", Version: "0.1.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_latest_price",
		Description: "Get the most recently collected close price for an asset.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.GetLatestPriceArgs) (*mcp.CallToolResult, tools.GetLatestPriceResult, error) {
		result, err := tools.GetLatestPrice(ctx, store, args)
		return nil, result, err
	})

	return server.Run(ctx, &mcp.StdioTransport{})
}
