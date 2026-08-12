package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/jmoiron/sqlx"
)

const indexPostsQuery = `
	SELECT p.id, p.user_id, p.body, p.mime, p.created_at
	FROM posts AS p
	INNER JOIN users AS u ON u.id = p.user_id AND u.del_flg = 0
	ORDER BY p.created_at DESC
	LIMIT ?`

// indexPostsCache stores the DB portion of the index page. User/session data
// and comments are still loaded per request because they are request-specific
// or can change independently of posts.
type indexPostsCache struct {
	mu    sync.Mutex
	posts []Post
	valid bool
}

func (c *indexPostsCache) load(ctx context.Context, conn *sqlx.DB) ([]Post, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.valid {
		return append([]Post(nil), c.posts...), nil
	}

	posts := []Post{}
	if err := conn.SelectContext(ctx, &posts, indexPostsQuery, postsPerPage); err != nil {
		return nil, fmt.Errorf("load index posts: %w", err)
	}
	c.posts = posts
	c.valid = true
	return append([]Post(nil), posts...), nil
}

func (c *indexPostsCache) invalidate() {
	c.mu.Lock()
	c.posts = nil
	c.valid = false
	c.mu.Unlock()
}
