package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/jmoiron/sqlx"
)

// accountProfileCache caches the two DB queries used by a user profile.
// Posts are immutable after creation, so the post list is invalidated only
// when that user's posts change. User entries are invalidated when visibility
// changes, such as an administrative ban.
type accountProfileCache struct {
	mu         sync.RWMutex
	users      map[string]User
	posts      map[int][]Post
	source     *sqlx.DB
	generation uint64
}

func (c *accountProfileCache) loadUser(ctx context.Context, conn *sqlx.DB, accountName string) (User, error) {
	c.mu.RLock()
	if c.source == conn {
		if user, ok := c.users[accountName]; ok {
			c.mu.RUnlock()
			return user, nil
		}
	}
	source, generation := c.source, c.generation
	c.mu.RUnlock()

	user := User{}
	if err := conn.GetContext(ctx, &user, "SELECT * FROM `users` WHERE `account_name` = ? AND `del_flg` = 0", accountName); err != nil {
		return User{}, err
	}

	c.mu.Lock()
	if c.source == source && c.generation == generation {
		c.ensureSourceLocked(conn)
		if c.users == nil {
			c.users = make(map[string]User)
		}
		c.users[accountName] = user
	}
	c.mu.Unlock()
	return user, nil
}

func (c *accountProfileCache) loadPosts(ctx context.Context, conn *sqlx.DB, userID int) ([]Post, error) {
	c.mu.RLock()
	if c.source == conn {
		if posts, ok := c.posts[userID]; ok {
			cached := append([]Post(nil), posts...)
			c.mu.RUnlock()
			return cached, nil
		}
	}
	source, generation := c.source, c.generation
	c.mu.RUnlock()

	posts := []Post{}
	if err := conn.SelectContext(ctx, &posts, "SELECT `id`, `user_id`, `body`, `mime`, `created_at` FROM `posts` WHERE `user_id` = ? ORDER BY `created_at` DESC", userID); err != nil {
		return nil, fmt.Errorf("load posts for user %d: %w", userID, err)
	}

	c.mu.Lock()
	if c.source == source && c.generation == generation {
		c.ensureSourceLocked(conn)
		if c.posts == nil {
			c.posts = make(map[int][]Post)
		}
		c.posts[userID] = append([]Post(nil), posts...)
	}
	c.mu.Unlock()
	return posts, nil
}

func (c *accountProfileCache) ensureSourceLocked(conn *sqlx.DB) {
	if c.source == conn {
		return
	}
	c.users = make(map[string]User)
	c.posts = make(map[int][]Post)
	c.source = conn
	c.generation++
}

func (c *accountProfileCache) invalidatePosts(userID int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.posts, userID)
	c.generation++
}

func (c *accountProfileCache) invalidateUser(userID int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for accountName, user := range c.users {
		if user.ID == userID {
			delete(c.users, accountName)
		}
	}
	delete(c.posts, userID)
	c.generation++
}

func (c *accountProfileCache) invalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.users = nil
	c.posts = nil
	c.source = nil
	c.generation++
}
