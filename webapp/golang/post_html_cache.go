package main

import (
	"html/template"
	"sync"
)

type postHTMLCacheKey struct {
	postID      int
	allComments bool
}

// postHTMLCache stores rendered post fragments so pagination can reuse the
// same post output across index and load-more requests.
type postHTMLCache struct {
	mu   sync.RWMutex
	html map[postHTMLCacheKey]template.HTML
}

func (c *postHTMLCache) load(postID int, allComments bool) (template.HTML, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	html, ok := c.html[postHTMLCacheKey{postID: postID, allComments: allComments}]
	return html, ok
}

func (c *postHTMLCache) store(postID int, allComments bool, html template.HTML) {
	c.mu.Lock()
	if c.html == nil {
		c.html = make(map[postHTMLCacheKey]template.HTML)
	}
	c.html[postHTMLCacheKey{postID: postID, allComments: allComments}] = html
	c.mu.Unlock()
}

func (c *postHTMLCache) invalidate(postIDs ...int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(postIDs) == 0 {
		c.html = nil
		return
	}
	for _, postID := range uniqueIDs(postIDs) {
		delete(c.html, postHTMLCacheKey{postID: postID, allComments: false})
		delete(c.html, postHTMLCacheKey{postID: postID, allComments: true})
	}
}
