package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/jmoiron/sqlx"
)

const postByIDQuery = "SELECT `id`, `user_id`, `body`, `mime`, `created_at` FROM `posts` WHERE `id` = ?"

type postCacheStore struct {
	mu     sync.RWMutex
	posts  map[int]Post
	source *sqlx.DB
}

func (c *postCacheStore) load(ctx context.Context, conn *sqlx.DB, postID int) ([]Post, error) {
	c.mu.RLock()
	if c.source == conn {
		if post, ok := c.posts[postID]; ok {
			c.mu.RUnlock()
			return []Post{post}, nil
		}
	}
	c.mu.RUnlock()

	posts := []Post{}
	if err := conn.SelectContext(ctx, &posts, postByIDQuery, postID); err != nil {
		return nil, fmt.Errorf("load post %d: %w", postID, err)
	}
	if len(posts) == 0 {
		return posts, nil
	}

	c.mu.Lock()
	if c.source != conn {
		c.posts = make(map[int]Post)
		c.source = conn
	}
	if c.posts == nil {
		c.posts = make(map[int]Post)
	}
	c.posts[postID] = posts[0]
	c.mu.Unlock()

	return posts, nil
}

func (c *postCacheStore) invalidate() {
	c.mu.Lock()
	c.posts = nil
	c.source = nil
	c.mu.Unlock()
}
