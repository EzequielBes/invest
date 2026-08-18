package agents

import (
	"context"
	"fmt"
	"strings"
	"time"

	"analysis/internal/llm"
	"analysis/internal/news"
	"analysis/internal/storage"
)

const newsSystemPrompt = `Você é um analista de notícias sobre criptomoedas. Dado uma lista de manchetes recentes sobre um ativo, escreva um resumo curto (2-4 frases) em português sobre o tom geral das notícias (positivo, negativo, neutro ou misto) e os principais temas. Se não houver notícias, diga isso claramente. Seja direto, sem recomendação de compra ou venda.`

func News(ctx context.Context, store *storage.Store, client llm.Client, symbol, name string) (Output, error) {
	rawItems, err := store.RecentNews(ctx, time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		return Output{}, fmt.Errorf("agents: news: fetch recent news: %w", err)
	}
	items := make([]news.Item, len(rawItems))
	for i, it := range rawItems {
		items[i] = news.Item{Title: it.Title, Body: it.Body, URL: it.URL, PublishedAt: it.PublishedAt}
	}
	result := news.Search(items, symbol, name)
	var userPrompt string
	if result.ArticleCount == 0 {
		userPrompt = fmt.Sprintf("Ativo: %s\nNenhuma notícia encontrada nas últimas 24 horas.", symbol)
	} else {
		var sb strings.Builder
		fmt.Fprintf(&sb, "Ativo: %s\n%d notícia(s) encontrada(s) nas últimas 24 horas:\n", symbol, result.ArticleCount)
		for _, article := range result.Articles {
			fmt.Fprintf(&sb, "- %s\n", article.Title)
		}
		userPrompt = sb.String()
	}
	narrative, err := client.Summarize(ctx, newsSystemPrompt, userPrompt)
	if err != nil {
		return Output{Indicators: result, Err: err}, nil
	}
	return Output{Indicators: result, Narrative: narrative}, nil
}
