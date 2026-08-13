package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/jmoiron/sqlx"
)

const (
	postsPageCacheLimit = 128
	postsPageQuery      = `
		SELECT p.id, p.user_id, p.body, p.mime, p.created_at
		FROM posts AS p
		INNER JOIN users AS u ON u.id = p.user_id AND u.del_flg = 0
		WHERE p.created_at <= ?
		ORDER BY p.created_at DESC
		LIMIT ?`
	postsPageMySQLQuery = `
		SELECT p.id, p.user_id, p.body, p.mime, p.created_at
		FROM posts AS p FORCE INDEX (idx_posts_created_at)
		INNER JOIN users AS u ON u.id = p.user_id AND u.del_flg = 0
		WHERE p.created_at <= ?
		ORDER BY p.created_at DESC
		LIMIT ?`
)

// postsPageCacheStore caches the DB portion of /posts for each pagination cursor.
// Cursors are shared by concurrent clients while the timeline is unchanged,
// so a bounded FIFO cache avoids repeating the same query without allowing
// unbounded growth when clients reach different pages.
type postsPageCacheStore struct {
	mu     sync.Mutex
	source *sqlx.DB
	pages  map[string][]Post
	order  []string
}

func (c *postsPageCacheStore) load(ctx context.Context, conn *sqlx.DB, maxCreatedAt string) ([]Post, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.source != conn {
		c.source = conn
		c.pages = make(map[string][]Post)
		c.order = nil
	}
	if posts, ok := c.pages[maxCreatedAt]; ok {
		return append([]Post(nil), posts...), nil
	}

	posts := []Post{}
	query := postsPageQuery
	if conn.DriverName() == "mysql" {
		query = postsPageMySQLQuery
	}
	if err := conn.SelectContext(ctx, &posts, query, maxCreatedAt, postsPerPage); err != nil {
		return nil, fmt.Errorf("load posts page at %s: %w", maxCreatedAt, err)
	}
	if len(c.order) >= postsPageCacheLimit {
		oldest := c.order[0]
		delete(c.pages, oldest)
		c.order = c.order[1:]
	}
	c.pages[maxCreatedAt] = append([]Post(nil), posts...)
	c.order = append(c.order, maxCreatedAt)
	return append([]Post(nil), posts...), nil
}

func (c *postsPageCacheStore) invalidate() {
	c.mu.Lock()
	c.source = nil
	c.pages = nil
	c.order = nil
	c.mu.Unlock()
}
