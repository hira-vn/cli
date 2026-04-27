package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Embedder generates vector embeddings for text inputs.
type Embedder interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
	Dimensions() int
}

// embeddingDim must match the vector(N) size in the migration.
const embeddingDim = 1536

// NewEmbedderFromEnv returns an OpenAI-backed embedder when OPENAI_API_KEY is
// set, and a deterministic no-op embedder otherwise. The no-op path lets the
// rest of the pipeline (chunking, extraction) work even without credentials,
// but semantic retrieval will degrade to keyword-only.
func NewEmbedderFromEnv() Embedder {
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return &openAIEmbedder{
			apiKey:     key,
			baseURL:    envOr("OPENAI_BASE_URL", "https://api.openai.com/v1"),
			model:      envOr("OPENAI_EMBEDDING_MODEL", "text-embedding-3-large"),
			httpClient: &http.Client{Timeout: 60 * time.Second},
		}
	}
	return &noopEmbedder{}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type openAIEmbedder struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

type openAIEmbedRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type openAIEmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func (e *openAIEmbedder) Dimensions() int { return embeddingDim }

func (e *openAIEmbedder) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	// OpenAI accepts up to ~100 inputs per call; we batch at 50 to stay safe.
	const batchSize = 50
	out := make([][]float32, len(inputs))
	for start := 0; start < len(inputs); start += batchSize {
		end := start + batchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		batch := inputs[start:end]

		reqBody, err := json.Marshal(openAIEmbedRequest{
			Model:      e.model,
			Input:      batch,
			Dimensions: embeddingDim,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal embed request: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			e.baseURL+"/embeddings", bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf("build embed request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+e.apiKey)

		resp, err := e.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("openai embed: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("openai embed http %d: %s", resp.StatusCode, string(body))
		}

		var parsed openAIEmbedResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("parse embed response: %w", err)
		}
		if parsed.Error != nil {
			return nil, fmt.Errorf("openai embed: %s", parsed.Error.Message)
		}
		if len(parsed.Data) != len(batch) {
			return nil, fmt.Errorf("openai embed: got %d embeddings for %d inputs",
				len(parsed.Data), len(batch))
		}
		for _, d := range parsed.Data {
			idx := start + d.Index
			if idx < 0 || idx >= len(out) {
				return nil, fmt.Errorf("openai embed: out of range index %d", d.Index)
			}
			out[idx] = d.Embedding
		}
	}
	return out, nil
}

// noopEmbedder returns zero vectors. Hybrid search falls back to BM25 when all
// embeddings are zero (cosine distance is meaningless), so keyword retrieval
// still works in dev without credentials.
type noopEmbedder struct{}

func (n *noopEmbedder) Dimensions() int { return embeddingDim }

func (n *noopEmbedder) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	out := make([][]float32, len(inputs))
	for i := range out {
		out[i] = make([]float32, embeddingDim)
	}
	return out, nil
}
