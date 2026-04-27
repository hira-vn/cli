package knowledge

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestStore_CreateDoc_NilTags regresses a constraint-violation bug: callers
// often omit the optional tags field, and we must default it to '{}' before
// binding the parameter to avoid tripping the NOT NULL constraint on
// knowledge_doc.tags.
//
// Skips when no test DB is reachable so contributor workflows that run
// `go test ./pkg/knowledge/...` without Postgres still pass.
func TestStore_CreateDoc_NilTags(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://hira:hira@localhost:5432/hira?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("DB not reachable: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("DB ping failed: %v", err)
	}

	// Create a throwaway workspace scoped to this test so we don't collide
	// with other fixtures.
	var wsID string
	err = pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, issue_prefix)
		VALUES ('kg-tags-test', 'kg-tags-test-' || substr(gen_random_uuid()::text, 1, 8), 'KGT')
		RETURNING id`).Scan(&wsID)
	if err != nil {
		t.Skipf("cannot insert workspace: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsID)

	store := NewStore(pool)
	doc, err := store.CreateDoc(ctx, CreateDocParams{
		WorkspaceID: wsID,
		Title:       "Hoàn tiền",
		Slug:        "hoan-tien",
		Category:    CategorySOP,
		// Tags intentionally omitted — this was the reproducer.
	})
	if err != nil {
		t.Fatalf("CreateDoc with nil tags failed: %v", err)
	}
	if doc.Tags == nil {
		t.Errorf("expected non-nil tags after read-back, got nil")
	}
	if len(doc.Tags) != 0 {
		t.Errorf("expected empty tags, got %v", doc.Tags)
	}
}
