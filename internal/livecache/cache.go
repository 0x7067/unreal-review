package livecache

import "sync"

type Cache struct {
	mu    sync.Mutex
	items map[string]string
}

func New() *Cache {
	return &Cache{items: map[string]string{}}
}

func (c *Cache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.items[key]
	return v, ok
}

func (c *Cache) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = value
}

func (c *Cache) Len() int {
	return len(c.items)
}

func (c *Cache) Delete(key string) {
	delete(c.items, key)
}
