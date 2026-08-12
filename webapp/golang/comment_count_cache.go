package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/jmoiron/sqlx"
)

type commentCountCacheStore struct {
	mu     sync.RWMutex
	counts map[int]int
	source *sqlx.DB
}

func (c *commentCountCacheStore) load(ctx context.Context, conn *sqlx.DB, postIDs []int) (map[int]int, error) {
	ids := uniqueIDs(postIDs)
	counts := make(map[int]int, len(ids))
	if len(ids) == 0 {
		return counts, nil
	}

	c.mu.RLock()
	missing := make([]int, 0, len(ids))
	if c.source == conn {
		for _, id := range ids {
			if count, ok := c.counts[id]; ok {
				counts[id] = count
			} else {
				missing = append(missing, id)
			}
		}
	} else {
		missing = append(missing, ids...)
	}
	c.mu.RUnlock()

	if len(missing) == 0 {
		return counts, nil
	}

	query, args, err := sqlx.In(`
		SELECT post_id, COUNT(*) AS count
		FROM comments
		WHERE post_id IN (?)
		GROUP BY post_id`, missing)
	if err != nil {
		return nil, fmt.Errorf("build comment count query: %w", err)
	}
	var rows []commentCountRow
	if err := conn.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("load comment counts: %w", err)
	}
	loaded := make(map[int]int, len(missing))
	for _, id := range missing {
		loaded[id] = 0
	}
	for _, row := range rows {
		loaded[row.PostID] = row.Count
	}

	c.mu.Lock()
	if c.source != conn {
		c.counts = make(map[int]int)
		c.source = conn
	}
	if c.counts == nil {
		c.counts = make(map[int]int)
	}
	for id, count := range loaded {
		c.counts[id] = count
		counts[id] = count
	}
	c.mu.Unlock()

	return counts, nil
}

func (c *commentCountCacheStore) invalidate(postIDs ...int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(postIDs) == 0 {
		c.counts = nil
		c.source = nil
		return
	}
	for _, id := range uniqueIDs(postIDs) {
		delete(c.counts, id)
	}
}
