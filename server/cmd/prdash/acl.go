package main

import (
	"fmt"
	"slices"
	"strings"
)

// grant is one access-allowed entry of a Windows DACL.
type grant struct {
	sid  string
	mask uint32
}

// strangers returns the SIDs that hold access in grants without being in allowed, in the order
// they appear and without repeats. An entry with an empty access mask grants nothing.
func strangers(grants []grant, allowed ...string) []string {
	var out []string
	for _, g := range grants {
		if g.mask != 0 && !slices.Contains(allowed, g.sid) && !slices.Contains(out, g.sid) {
			out = append(out, g.sid)
		}
	}
	return out
}

// aclError is the refusal for a path whose DACL names other principals.
func aclError(path string, others []string) error {
	return fmt.Errorf("%s grants access to accounts other than you, SYSTEM and Administrators (%s); "+
		"run: icacls \"%s\" /inheritance:r /grant:r \"%%USERNAME%%:(OI)(CI)F\" /grant:r SYSTEM:(OI)(CI)F /grant:r Administrators:(OI)(CI)F",
		path, strings.Join(others, ", "), path)
}
