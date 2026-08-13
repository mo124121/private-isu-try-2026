package main

import (
	"html/template"
	"sync"
)

const indexCSRFPlaceholder = "__ISUCON_INDEX_CSRF_TOKEN__"

// indexPostsHTMLCache avoids executing post.html for every index request.
// The cached fragment contains a placeholder for the request-specific CSRF
// token and is invalidated whenever post-visible data changes.
type indexPostsHTMLCache struct {
	mu    sync.RWMutex
	html  template.HTML
	valid bool
}

func (c *indexPostsHTMLCache) load() (template.HTML, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.html, c.valid
}

func (c *indexPostsHTMLCache) store(html template.HTML) {
	c.mu.Lock()
	c.html = html
	c.valid = true
	c.mu.Unlock()
}

func (c *indexPostsHTMLCache) invalidate() {
	c.mu.Lock()
	c.html = ""
	c.valid = false
	c.mu.Unlock()
}
