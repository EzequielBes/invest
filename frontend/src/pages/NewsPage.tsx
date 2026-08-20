import { api, type NewsItem } from '../api/client';
import { usePolling } from '../hooks/usePolling';

export default function NewsPage() {
  const { data, error } = usePolling<NewsItem[]>(api.news);

  if (error) return <p className="error" role="alert">Erro ao carregar noticias: {error}</p>;
  if (!data) return <p aria-live="polite">Carregando...</p>;
  if (data.length === 0) return <p>Nenhuma noticia coletada ainda.</p>;

  return (
    <div className="news-list">
      {data.map((item) => (
        <article className="news-item" key={item.id}>
          <a href={item.url} rel="noreferrer" target="_blank">{item.title}</a>
          <div className="ledger-meta">
            <span>{item.source}</span>
            <span>{new Date(item.published_at).toLocaleString('pt-BR')}</span>
          </div>
          {item.body && <p>{item.body.length > 240 ? `${item.body.slice(0, 240)}...` : item.body}</p>}
        </article>
      ))}
    </div>
  );
}
