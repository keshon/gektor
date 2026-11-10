package tools

import "testing"

// Each permission scenario gets fresh tools constructed under that policy.
func policyCatalog(t *testing.T, dependencies Dependencies) *Capabilities {
	t.Helper()
	c := &Capabilities{}
	r, err := Compose(dependencies, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Registry = r
	for _, p := range c.Native {
		if _, err := c.Enable(t.Context(), p.ID, nil); err != nil {
			t.Fatal(err)
		}
	}
	return c
}
