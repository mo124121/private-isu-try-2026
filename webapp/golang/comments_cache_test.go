package main

import (
	"context"
	"testing"
	"time"
)

func TestCommentsCacheModesAndInvalidationWithSQLite(t *testing.T) {
	db := newMakePostsTestDB(t)
	cache := commentsCacheStore{}
	ctx := context.Background()

	latest, err := cache.load(ctx, db, []int{1}, false)
	if err != nil {
		t.Fatalf("load latest comments: %v", err)
	}
	if got := []int{latest[1][0].ID, latest[1][1].ID, latest[1][2].ID}; !equalInts(got, []int{2, 3, 4}) {
		t.Fatalf("unexpected latest comments: %v", got)
	}

	all, err := cache.load(ctx, db, []int{1}, true)
	if err != nil {
		t.Fatalf("load all comments: %v", err)
	}
	if len(all[1]) != 4 {
		t.Fatalf("expected 4 comments, got %d", len(all[1]))
	}

	if _, err := db.Exec(
		"INSERT INTO comments (id, post_id, user_id, comment, created_at) VALUES (?, ?, ?, ?, ?)",
		5, 1, 1, "new", time.Now(),
	); err != nil {
		t.Fatalf("insert comment fixture: %v", err)
	}
	latest, err = cache.load(ctx, db, []int{1}, false)
	if err != nil {
		t.Fatalf("load cached latest comments: %v", err)
	}
	if len(latest[1]) != 3 || latest[1][2].ID != 4 {
		t.Fatalf("expected cached latest comments before invalidation: %#v", latest[1])
	}

	cache.invalidate(1)
	latest, err = cache.load(ctx, db, []int{1}, false)
	if err != nil {
		t.Fatalf("load invalidated latest comments: %v", err)
	}
	if got := []int{latest[1][0].ID, latest[1][1].ID, latest[1][2].ID}; !equalInts(got, []int{3, 4, 5}) {
		t.Fatalf("unexpected invalidated latest comments: %v", got)
	}
	all, err = cache.load(ctx, db, []int{1}, true)
	if err != nil {
		t.Fatalf("load invalidated all comments: %v", err)
	}
	if len(all[1]) != 5 {
		t.Fatalf("expected invalidated all comments to contain 5 rows, got %d", len(all[1]))
	}
}
