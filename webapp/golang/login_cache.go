package main

import (
	"context"
	"sync"

	"github.com/jmoiron/sqlx"
)

type loginCacheStore struct {
	mu     sync.RWMutex
	users  map[string]User
	source *sqlx.DB
}

func (c *loginCacheStore) load(ctx context.Context, conn *sqlx.DB, accountName string) (*User, error) {
	c.mu.RLock()
	if c.source == conn {
		if user, ok := c.users[accountName]; ok {
			c.mu.RUnlock()
			cached := user
			return &cached, nil
		}
	}
	c.mu.RUnlock()

	user := User{}
	if err := conn.GetContext(ctx, &user, "SELECT * FROM users WHERE account_name = ? AND del_flg = 0", accountName); err != nil {
		return nil, nil
	}

	c.mu.Lock()
	if c.source != conn {
		c.users = make(map[string]User)
		c.source = conn
	}
	if c.users == nil {
		c.users = make(map[string]User)
	}
	c.users[accountName] = user
	c.mu.Unlock()

	return &user, nil
}

func (c *loginCacheStore) invalidate(accountNames ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(accountNames) == 0 {
		c.users = nil
		c.source = nil
		return
	}
	for _, accountName := range accountNames {
		delete(c.users, accountName)
	}
}
