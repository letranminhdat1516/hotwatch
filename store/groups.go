package store

import "fmt"

// RemoveGroup deletes the group. Its hosts and child groups move to the removed
// group's parent; with no parent they become ungrouped ("") / top level.
// Unknown ids are a no-op.
func (c *Config) RemoveGroup(id string) {
	idx := -1
	for i, g := range c.Groups {
		if g.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	parent := c.Groups[idx].Parent
	c.Groups = append(c.Groups[:idx:idx], c.Groups[idx+1:]...)
	for i := range c.Groups {
		if c.Groups[i].Parent == id {
			c.Groups[i].Parent = parent
		}
	}
	for i := range c.Hosts {
		if c.Hosts[i].Group == id {
			c.Hosts[i].Group = parent
		}
	}
	open := c.UI.OpenGroups[:0:0]
	for _, g := range c.UI.OpenGroups {
		if g != id {
			open = append(open, g)
		}
	}
	c.UI.OpenGroups = open
}

// MoveHost puts a host in a group; groupID "" means root. Unknown ids error.
func (c *Config) MoveHost(hostID, groupID string) error {
	hi := -1
	for i, h := range c.Hosts {
		if h.ID == hostID {
			hi = i
			break
		}
	}
	if hi < 0 {
		return fmt.Errorf("unknown host %q", hostID)
	}
	if groupID != "" {
		found := false
		for _, g := range c.Groups {
			if g.ID == groupID {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown group %q", groupID)
		}
	}
	c.Hosts[hi].Group = groupID
	return nil
}
