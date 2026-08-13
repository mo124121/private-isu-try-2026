package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/jmoiron/sqlx"
)

type userCommentCountCacheStore struct {
	mu     sync.RWMutex
	counts map[int]int
	source *sqlx.DB
}

func (c *userCommentCountCacheStore) load(ctx context.Context, conn *sqlx.DB, userID int) (int, error) {
	c.mu.RLock()
	if c.source == conn {
		if count, ok := c.counts[userID]; ok {
			c.mu.RUnlock()
			return count, nil
		}
	}
	c.mu.RUnlock()

	count := 0
	if err := conn.GetContext(ctx, &count, "SELECT COUNT(*) FROM comments WHERE user_id = ?", userID); err != nil {
		return 0, fmt.Errorf("load comment count for user %d: %w", userID, err)
	}

	c.mu.Lock()
	if c.source != conn {
		c.counts = make(map[int]int)
		c.source = conn
	}
	if c.counts == nil {
		c.counts = make(map[int]int)
	}
	c.counts[userID] = count
	c.mu.Unlock()

	return count, nil
}

func (c *userCommentCountCacheStore) invalidate(userIDs ...int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(userIDs) == 0 {
		c.counts = nil
		c.source = nil
		return
	}
	for _, userID := range uniqueIDs(userIDs) {
		delete(c.counts, userID)
	}
}
