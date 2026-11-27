package jira

// CachedTransitions is the number of ticket entries in the transitions cache.
func (c *Client) CachedTransitions() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.transitions)
}
