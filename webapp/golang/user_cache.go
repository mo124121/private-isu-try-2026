package main

import (
	"context"
	"sync"

	"github.com/jmoiron/sqlx"
)

// userCache keeps user rows shared by post owners and comment authors.
// User rows are immutable for normal requests; administrative visibility
// changes explicitly invalidate the affected entries.
type userCacheStore struct {
	mu     sync.RWMutex
	users  map[int]User
	source *sqlx.DB
}

func (c *userCacheStore) load(ctx context.Context, conn *sqlx.DB, userIDs []int) (map[int]User, error) {
	ids := uniqueIDs(userIDs)
	if len(ids) == 0 {
		return map[int]User{}, nil
	}

	c.mu.RLock()
	if c.source == conn {
		cached := make(map[int]User, len(ids))
		missing := make([]int, 0)
		for _, id := range ids {
			if user, ok := c.users[id]; ok {
				cached[id] = user
			} else {
				missing = append(missing, id)
			}
		}
		c.mu.RUnlock()
		if len(missing) == 0 {
			return cached, nil
		}
		return c.loadMissing(ctx, conn, cached, missing)
	}
	c.mu.RUnlock()

	return c.loadMissing(ctx, conn, make(map[int]User, len(ids)), ids)
}

func (c *userCacheStore) loadMissing(ctx context.Context, conn *sqlx.DB, users map[int]User, missing []int) (map[int]User, error) {
	query, args, err := sqlx.In("SELECT * FROM users WHERE id IN (?)", missing)
	if err != nil {
		return nil, err
	}

	var rows []User
	if err := conn.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}

	c.mu.Lock()
	if c.source != conn {
		c.users = make(map[int]User)
		c.source = conn
	}
	if c.users == nil {
		c.users = make(map[int]User)
	}
	for _, user := range rows {
		c.users[user.ID] = user
		users[user.ID] = user
	}
	c.mu.Unlock()

	return users, nil
}

func (c *userCacheStore) invalidate(userIDs ...int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(userIDs) == 0 {
		c.users = nil
		c.source = nil
		return
	}
	for _, id := range uniqueIDs(userIDs) {
		delete(c.users, id)
	}
}
