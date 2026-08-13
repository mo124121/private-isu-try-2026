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

func TestAccountProfileCacheAppendPost(t *testing.T) {
	db := newMakePostsTestDB(t)
	cache := accountProfileCache{}
	ctx := context.Background()

	if _, err := cache.loadPosts(ctx, db, 1); err != nil {
		t.Fatalf("warm profile posts cache: %v", err)
	}

	newPost := Post{
		ID:        10,
		UserID:    1,
		Body:      "new post",
		Mime:      "image/png",
		CreatedAt: time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC),
	}
	cache.appendPost(newPost)
	posts, err := cache.loadPosts(ctx, db, 1)
	if err != nil {
		t.Fatalf("load appended profile posts: %v", err)
	}
	if len(posts) != 2 || posts[0].ID != newPost.ID {
		t.Fatalf("expected appended post at the front, got %#v", posts)
	}

	cache.appendPost(newPost)
	posts, err = cache.loadPosts(ctx, db, 1)
	if err != nil {
		t.Fatalf("load deduplicated profile posts: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("expected duplicate append to be ignored, got %d posts", len(posts))
	}

	cache.appendPost(Post{ID: 11, UserID: 2})
	posts, err = cache.loadPosts(ctx, db, 2)
	if err != nil {
		t.Fatalf("load uncached profile posts: %v", err)
	}
	if len(posts) != 1 || posts[0].ID != 2 {
		t.Fatalf("uncached user should not receive partial cache, got %#v", posts)
	}
}
