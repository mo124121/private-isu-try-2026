package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/jmoiron/sqlx"
)

type commentsCacheKey struct {
	postID      int
	allComments bool
}

// commentsCache stores comment rows without resolved User values. User data is
// deliberately resolved by userCache on every load so a user ban remains
// visible without having to invalidate every comment entry they authored.
type commentsCacheStore struct {
	mu         sync.RWMutex
	comments   map[commentsCacheKey][]Comment
	source     *sqlx.DB
	generation uint64
}

func (c *commentsCacheStore) load(ctx context.Context, conn *sqlx.DB, postIDs []int, allComments bool) (map[int][]Comment, error) {
	ids := uniqueIDs(postIDs)
	result := make(map[int][]Comment, len(ids))
	if len(ids) == 0 {
		return result, nil
	}

	missing := make([]int, 0, len(ids))
	c.mu.RLock()
	if c.source == conn {
		for _, id := range ids {
			key := commentsCacheKey{postID: id, allComments: allComments}
			comments, ok := c.comments[key]
			if !ok {
				missing = append(missing, id)
				continue
			}
			result[id] = append([]Comment(nil), comments...)
		}
	} else {
		missing = append(missing, ids...)
	}
	source, generation := c.source, c.generation
	c.mu.RUnlock()

	if len(missing) == 0 {
		return result, nil
	}

	query := `
		SELECT id, post_id, user_id, comment, created_at
		FROM comments
		WHERE post_id IN (?)
		ORDER BY post_id ASC, created_at ASC, id ASC`
	if !allComments {
		query = `
			SELECT id, post_id, user_id, comment, created_at
			FROM (
				SELECT id, post_id, user_id, comment, created_at,
					ROW_NUMBER() OVER (
						PARTITION BY post_id
						ORDER BY created_at DESC, id DESC
					) AS row_num
				FROM comments
				WHERE post_id IN (?)
			) AS ranked_comments
			WHERE row_num <= 3
			ORDER BY post_id ASC, created_at ASC, id ASC`
	}

	query, args, err := sqlx.In(query, missing)
	if err != nil {
		return nil, fmt.Errorf("build comments query: %w", err)
	}
	var rows []Comment
	if err := conn.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("load comments: %w", err)
	}

	loaded := make(map[int][]Comment, len(missing))
	for _, comment := range rows {
		loaded[comment.PostID] = append(loaded[comment.PostID], comment)
	}
	for _, id := range missing {
		if _, ok := loaded[id]; !ok {
			loaded[id] = []Comment{}
		}
	}

	c.mu.Lock()
	if c.source == source && c.generation == generation {
		c.ensureSourceLocked(conn)
		if c.comments == nil {
			c.comments = make(map[commentsCacheKey][]Comment)
		}
		for id, comments := range loaded {
			key := commentsCacheKey{postID: id, allComments: allComments}
			c.comments[key] = append([]Comment(nil), comments...)
		}
	}
	c.mu.Unlock()

	for id, comments := range loaded {
		result[id] = append([]Comment(nil), comments...)
	}
	return result, nil
}

func (c *commentsCacheStore) ensureSourceLocked(conn *sqlx.DB) {
	if c.source == conn {
		return
	}
	c.comments = make(map[commentsCacheKey][]Comment)
	c.source = conn
	c.generation++
}

func (c *commentsCacheStore) invalidate(postID int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.comments, commentsCacheKey{postID: postID, allComments: false})
	delete(c.comments, commentsCacheKey{postID: postID, allComments: true})
	c.generation++
}

func (c *commentsCacheStore) invalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.comments = nil
	c.source = nil
	c.generation++
}
