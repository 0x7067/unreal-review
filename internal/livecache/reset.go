package livecache

func (c *Cache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = nil
}
