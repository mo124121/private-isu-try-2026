package main

import (
	"context"
	"testing"
	"time"
)

func TestAccountProfileCacheInvalidationWithSQLite(t *testing.T) {
	db := newMakePostsTestDB(t)
	cache := accountProfileCache{}
	ctx := context.Background()

	user, err := cache.loadUser(ctx, db, "alice")
	if err != nil {
		t.Fatalf("load profile user: %v", err)
	}
	if user.ID != 1 {
		t.Fatalf("expected alice to have ID 1, got %d", user.ID)
	}

	posts, err := cache.loadPosts(ctx, db, user.ID)
	if err != nil {
		t.Fatalf("load profile posts: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("expected 1 initial post, got %d", len(posts))
	}

	if _, err := db.Exec(
		"INSERT INTO posts (id, user_id, imgdata, body, mime, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		4, 1, []byte{}, "new", "text/plain", time.Now(),
	); err != nil {
		t.Fatalf("insert post fixture: %v", err)
	}
	posts, err = cache.loadPosts(ctx, db, user.ID)
	if err != nil {
		t.Fatalf("load cached profile posts: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("expected cached 1 post before invalidation, got %d", len(posts))
	}

	cache.invalidatePosts(user.ID)
	posts, err = cache.loadPosts(ctx, db, user.ID)
	if err != nil {
		t.Fatalf("load invalidated profile posts: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("expected 2 posts after invalidation, got %d", len(posts))
	}

	if _, err := db.Exec("UPDATE users SET del_flg = 1 WHERE id = ?", user.ID); err != nil {
		t.Fatalf("ban user fixture: %v", err)
	}
	if _, err := cache.loadUser(ctx, db, "alice"); err != nil {
		t.Fatalf("expected cached user before invalidation, got %v", err)
	}

	cache.invalidateUser(user.ID)
	if _, err := cache.loadUser(ctx, db, "alice"); err == nil {
		t.Fatal("expected banned user lookup to fail after invalidation")
	}
}
