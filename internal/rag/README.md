# Hybrid retrieval

`Read` and `Load` keep the legacy BM25 behavior until `WithHybrid(HybridConfig)`
is called. The application supplies an installed Ollama embedding model and a
loopback HTTP(S) origin. `Prepare(ctx)` builds the index in a cancellable startup
job. A search can prepare missing eligible documents itself, with an eight-second
default budget including query embedding and ranking.

The HTTP path is Ollama `POST /api/embed`, with `input` as an array and
`truncate: false`. Vectors are real model outputs; invalid, zero, non-finite,
inconsistent or oversized vectors are rejected. EmbeddingGemma uses its retrieval
document/query prefixes. Other models receive plain text. Long documents are
split at UTF-8/word boundaries into 1,200-byte text parts, each with its title.
Every part is embedded, so the tail of a chunk is retained. Query parts are at most
1,400 bytes. The maximum cosine over part pairs ranks the original whole chunk.

Agent, mission-fact and topic filters run before both BM25 statistics/ranking and
vector candidate selection. Background preparation can index the full trusted
local corpus; this never expands the candidates available to a search. Lazy
search sends only eligible documents to the local embedding service. `Scope`
shares the index safely but retains its own immutable permission facts.

The two rankings are combined with reciprocal rank fusion (`k = 60`, equal
weights, at most `max(20, limit * 4)` candidates per ranking). Nonpositive cosine
matches are excluded. Cosine is a retrieval signal, not a factual-confidence
score; citations and answer grounding remain the caller's responsibility.

Each result reports the algorithm actually used:

- `bm25`: embeddings were not used, including unconfigured/empty searches.
- `hybrid`: the query embedding succeeded and vector ranks were fused with BM25.
- `bm25_fallback`: the configured embedding path failed; `degraded_reason` is a
  stable error code. Caller cancellation remains a returned error.

`HybridStatus` exposes only model, document counts and sanitized status text. A
cache write failure can report `ready: true` with `embedding_cache_unwritable`:
vectors still work in memory. A service/model/query failure reports `ready: false`
until a successful preparation or search. An on-disk ready index alone does not
prove a currently available Ollama service; actual search metadata is authoritative.

The JSON cache contains normalized vectors, the exact model name, a SHA-256 corpus
fingerprint, and individual SHA-256 embedding-input fingerprints; it contains no
source text or queries. Writes use a private temporary file and atomic replacement
with `0600` permissions. App/MCP readers validate the complete file before reuse.
Concurrent processes merge matching entries before writing. A simultaneous write
may lose cached work and cause recomputation, but cannot mix incompatible vectors
or expose a partial file. Replacing custom model weights under the same Ollama tag
requires deleting the cache or choosing a new tag.

The implementation uses only the Go standard library. `hybrid_test.go` exercises
the actual HTTP request/response code through an injected transport, including
semantic-only retrieval, filtering, RRF, long-chunk tails, cache identity and
corruption, unavailable models, invalid vectors, cancellation and concurrent
scoped searches. It does not claim a live Ollama or model-quality benchmark.

```sh
go test ./internal/rag
go test -race ./internal/rag
```
