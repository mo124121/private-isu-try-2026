package main

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

func newMakePostsTestDB(t *testing.T) *sqlx.DB {
	t.Helper()

	db, err := sqlx.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite test database: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })

	statements := []string{
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			account_name TEXT NOT NULL,
			passhash TEXT NOT NULL,
			authority INTEGER NOT NULL,
			del_flg INTEGER NOT NULL,
			created_at DATETIME NOT NULL
		)`,
		`CREATE TABLE posts (
			id INTEGER PRIMARY KEY,
			user_id INTEGER NOT NULL,
			imgdata BLOB,
			body TEXT NOT NULL,
			mime TEXT NOT NULL,
			created_at DATETIME NOT NULL
		)`,
		`CREATE TABLE comments (
			id INTEGER PRIMARY KEY,
			post_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			comment TEXT NOT NULL,
			created_at DATETIME NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("create sqlite test schema: %v", err)
		}
	}

	createdAt := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	users := []struct {
		id          int
		accountName string
		delFlg      int
	}{
		{id: 1, accountName: "alice", delFlg: 0},
		{id: 2, accountName: "bob", delFlg: 0},
		{id: 3, accountName: "deleted", delFlg: 1},
	}
	for _, user := range users {
		_, err := db.Exec(
			"INSERT INTO users (id, account_name, passhash, authority, del_flg, created_at) VALUES (?, ?, ?, ?, ?, ?)",
			user.id, user.accountName, "passhash", 0, user.delFlg, createdAt,
		)
		if err != nil {
			t.Fatalf("insert user fixture: %v", err)
		}
	}

	posts := []struct {
		id     int
		userID int
		body   string
	}{
		{id: 1, userID: 1, body: "first"},
		{id: 2, userID: 2, body: "second"},
		{id: 3, userID: 3, body: "hidden"},
	}
	for _, post := range posts {
		_, err := db.Exec(
			"INSERT INTO posts (id, user_id, imgdata, body, mime, created_at) VALUES (?, ?, ?, ?, ?, ?)",
			post.id, post.userID, []byte{}, post.body, "text/plain", createdAt,
		)
		if err != nil {
			t.Fatalf("insert post fixture: %v", err)
		}
	}

	for id := 1; id <= 4; id++ {
		_, err := db.Exec(
			"INSERT INTO comments (id, post_id, user_id, comment, created_at) VALUES (?, ?, ?, ?, ?)",
			id, 1, 1+(id%2), "comment", createdAt.Add(time.Duration(id)*time.Minute),
		)
		if err != nil {
			t.Fatalf("insert comment fixture: %v", err)
		}
	}

	return db
}

func TestLoadCommentCountsWithSQLite(t *testing.T) {
	db := newMakePostsTestDB(t)

	counts, err := loadCommentCounts(context.Background(), db, []int{1, 1, 2})
	if err != nil {
		t.Fatalf("loadCommentCounts: %v", err)
	}
	if counts[1] != 4 {
		t.Fatalf("expected 4 comments for post 1, got %d", counts[1])
	}
	if counts[2] != 0 {
		t.Fatalf("expected 0 comments for post 2, got %d", counts[2])
	}
}

func TestLoadCommentsWithSQLite(t *testing.T) {
	db := newMakePostsTestDB(t)

	comments, err := loadComments(context.Background(), db, []int{1, 1, 2}, false)
	if err != nil {
		t.Fatalf("loadComments: %v", err)
	}
	if got := []int{comments[1][0].ID, comments[1][1].ID, comments[1][2].ID}; !equalInts(got, []int{2, 3, 4}) {
		t.Fatalf("expected latest 3 comments in display order, got %v", got)
	}

	allComments, err := loadComments(context.Background(), db, []int{1}, true)
	if err != nil {
		t.Fatalf("loadComments(all): %v", err)
	}
	if got := []int{allComments[1][0].ID, allComments[1][3].ID}; !equalInts(got, []int{1, 4}) {
		t.Fatalf("expected all comments in display order, got %v", got)
	}
}

func TestLoadUsersWithSQLite(t *testing.T) {
	db := newMakePostsTestDB(t)

	users, err := loadUsers(context.Background(), db, []int{1, 1, 3})
	if err != nil {
		t.Fatalf("loadUsers: %v", err)
	}
	if users[1].AccountName != "alice" {
		t.Fatalf("expected alice, got %q", users[1].AccountName)
	}
	if users[3].DelFlg != 1 {
		t.Fatalf("expected deleted user flag, got %d", users[3].DelFlg)
	}
}

func TestUserCacheInvalidationWithSQLite(t *testing.T) {
	db := newMakePostsTestDB(t)
	cache := userCacheStore{}
	ctx := context.Background()

	users, err := cache.load(ctx, db, []int{1})
	if err != nil {
		t.Fatalf("load users: %v", err)
	}
	if users[1].AccountName != "alice" {
		t.Fatalf("expected alice, got %q", users[1].AccountName)
	}

	if _, err := db.Exec("UPDATE users SET account_name = ? WHERE id = ?", "alice-updated", 1); err != nil {
		t.Fatalf("update user fixture: %v", err)
	}
	users, err = cache.load(ctx, db, []int{1})
	if err != nil {
		t.Fatalf("load cached user: %v", err)
	}
	if users[1].AccountName != "alice" {
		t.Fatalf("expected cached alice before invalidation, got %q", users[1].AccountName)
	}

	cache.invalidate(1)
	users, err = cache.load(ctx, db, []int{1})
	if err != nil {
		t.Fatalf("load invalidated user: %v", err)
	}
	if users[1].AccountName != "alice-updated" {
		t.Fatalf("expected updated user after invalidation, got %q", users[1].AccountName)
	}
}

func TestLoadPostsWithSQLite(t *testing.T) {
	db := newMakePostsTestDB(t)
	ctx := context.Background()
	results := []Post{
		{ID: 1, UserID: 1, Body: "first"},
		{ID: 2, UserID: 2, Body: "second"},
		{ID: 3, UserID: 3, Body: "hidden"},
	}

	posts, err := loadPosts(ctx, db, results, "csrf-token", false)
	if err != nil {
		t.Fatalf("loadPosts: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("expected 2 visible posts, got %d", len(posts))
	}
	if posts[0].CommentCount != 4 {
		t.Fatalf("expected all 4 comments to be counted, got %d", posts[0].CommentCount)
	}
	if posts[0].User.AccountName != "alice" {
		t.Fatalf("expected post owner alice, got %q", posts[0].User.AccountName)
	}
	if posts[0].CSRFToken != "csrf-token" {
		t.Fatalf("expected csrf token to be propagated, got %q", posts[0].CSRFToken)
	}

	if got := []int{posts[0].Comments[0].ID, posts[0].Comments[1].ID, posts[0].Comments[2].ID}; !equalInts(got, []int{2, 3, 4}) {
		t.Fatalf("expected latest 3 comments in display order, got %v", got)
	}
	if posts[0].Comments[0].User.ID == 0 {
		t.Fatal("expected comment user to be loaded")
	}
}

func TestLoadPostsWithSQLiteLoadsAllCommentsForPost(t *testing.T) {
	db := newMakePostsTestDB(t)

	posts, err := loadPosts(context.Background(), db, []Post{{ID: 1, UserID: 1}}, "", true)
	if err != nil {
		t.Fatalf("loadPosts: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(posts))
	}
	if len(posts[0].Comments) != 4 {
		t.Fatalf("expected all 4 comments, got %d", len(posts[0].Comments))
	}
	if got := []int{posts[0].Comments[0].ID, posts[0].Comments[3].ID}; !equalInts(got, []int{1, 4}) {
		t.Fatalf("expected comments in display order, got %v", got)
	}
}

func TestIndexPostsCacheInvalidationWithSQLite(t *testing.T) {
	db := newMakePostsTestDB(t)
	cache := indexPostsCache{}

	posts, err := cache.load(context.Background(), db)
	if err != nil {
		t.Fatalf("load index posts: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("expected 2 visible posts, got %d", len(posts))
	}

	if _, err := db.Exec("DELETE FROM posts WHERE id = ?", 2); err != nil {
		t.Fatalf("delete post fixture: %v", err)
	}
	posts, err = cache.load(context.Background(), db)
	if err != nil {
		t.Fatalf("load cached index posts: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("expected cached 2 posts before invalidation, got %d", len(posts))
	}

	cache.invalidate()
	posts, err = cache.load(context.Background(), db)
	if err != nil {
		t.Fatalf("load invalidated index posts: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("expected 1 post after invalidation, got %d", len(posts))
	}
}

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
