package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/recipe"
)

// Path locates the reference within Field, separately from its source Pointer.
type Connection struct {
	From, To, Field, Path, Pointer string
}

func (c Connection) Label() string {
	if c.Field == "depends_on" {
		return c.From + " → " + c.To + "  run after"
	}
	pointer := c.Pointer
	if pointer == "" {
		pointer = "(whole output)"
	}
	return c.From + " → " + c.To + "  " + c.Field + c.Path + " ← " + pointer
}

func escapePointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

func pointerParts(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("JSON pointer must be empty or start with /")
	}
	parts := strings.Split(path[1:], "/")
	for i, p := range parts {
		if strings.Contains(strings.NewReplacer("~0", "", "~1", "").Replace(p), "~") {
			return nil, fmt.Errorf("invalid JSON pointer escape")
		}
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~")
	}
	return parts, nil
}

// Connections lists incoming and outgoing links, including nested references
// and condition/iteration dependencies. Empty selected lists the entire recipe.
func (e *Editor) Connections(selected string) []Connection {
	var out []Connection
	for _, step := range e.Definition.Steps {
		add := func(c Connection) {
			if selected == "" || c.From == selected || c.To == selected {
				out = append(out, c)
			}
		}
		for i, from := range step.DependsOn {
			add(Connection{From: from, To: step.ID, Field: "depends_on", Path: "/" + strconv.Itoa(i)})
		}
		var walk func(json.RawMessage, string, string)
		walk = func(raw json.RawMessage, field, path string) {
			var object map[string]json.RawMessage
			if json.Unmarshal(raw, &object) == nil && object != nil {
				var from, pointer string
				if json.Unmarshal(object["$step"], &from) == nil && from != "" {
					_ = json.Unmarshal(object["pointer"], &pointer)
					add(Connection{from, step.ID, field, path, pointer})
					return
				}
				keys := make([]string, 0, len(object))
				for key := range object {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					walk(object[key], field, path+"/"+escapePointer(key))
				}
				return
			}
			var array []json.RawMessage
			if json.Unmarshal(raw, &array) == nil {
				for i, v := range array {
					walk(v, field, path+"/"+strconv.Itoa(i))
				}
			}
		}
		walk(step.Args, "args", "")
		walk(step.When, "when", "")
		walk(step.ForEach, "for_each", "")
	}
	return out
}

// ChangeConnection performs one undoable change. Removing a required value is
// allowed: validation must report it, rather than inventing a replacement.
func (e *Editor) ChangeConnection(old, next *Connection) error {
	if old == nil && next == nil {
		return fmt.Errorf("no connection selected")
	}
	if old != nil {
		found := false
		for _, c := range e.Connections("") {
			if c == *old {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("connection changed; reopen the connection list")
		}
	}
	d := e.Clone()
	if old != nil && !(next != nil && old.To == next.To && old.Field == next.Field && old.Path == next.Path && old.Field != "depends_on") {
		if err := changeConnection(&d, *old, true); err != nil {
			return err
		}
	}
	if next != nil {
		if err := changeConnection(&d, *next, false); err != nil {
			return err
		}
	}
	if _, err := recipe.Order(d.Steps); err != nil {
		return err
	}
	if e.Source() == source(d) {
		return nil
	}
	e.Replace(d)
	return nil
}

func source(d workflow.Definition) string {
	b, _ := json.MarshalIndent(d, "", "  ")
	return string(b)
}

func changeConnection(d *workflow.Definition, c Connection, remove bool) error {
	if !remove {
		if _, err := pointerParts(c.Pointer); err != nil {
			return err
		}
		found := false
		for _, s := range d.Steps {
			if s.ID == c.From {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("source node missing")
		}
	}
	for i := range d.Steps {
		s := &d.Steps[i]
		if s.ID != c.To {
			continue
		}
		if c.Field == "depends_on" {
			if c.Pointer != "" {
				return fmt.Errorf("ordering connections have no output pointer")
			}
			if remove {
				index, err := strconv.Atoi(strings.TrimPrefix(c.Path, "/"))
				if err != nil || index < 0 || index >= len(s.DependsOn) {
					return fmt.Errorf("dependency missing")
				}
				s.DependsOn = append(s.DependsOn[:index], s.DependsOn[index+1:]...)
			} else {
				for _, from := range s.DependsOn {
					if from == c.From {
						return nil
					}
				}
				s.DependsOn = append(s.DependsOn, c.From)
			}
			return nil
		}
		var raw *json.RawMessage
		switch c.Field {
		case "args":
			raw = &s.Args
		case "when":
			raw = &s.When
		case "for_each":
			raw = &s.ForEach
		default:
			return fmt.Errorf("unknown connection field")
		}
		parts, err := pointerParts(c.Path)
		if err != nil {
			return err
		}
		ref := map[string]string{"$step": c.From}
		if c.Pointer != "" {
			ref["pointer"] = c.Pointer
		}
		value, _ := json.Marshal(ref)
		updated, err := updateValue(*raw, parts, value, remove)
		if err != nil {
			return err
		}
		if c.Field == "args" && len(updated) == 0 {
			updated = json.RawMessage(`{}`)
		}
		*raw = updated
		return nil
	}
	return fmt.Errorf("destination node missing")
}

func updateValue(raw json.RawMessage, parts []string, value json.RawMessage, remove bool) (json.RawMessage, error) {
	if len(parts) == 0 {
		if remove {
			return nil, nil
		}
		return value, nil
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) == nil && object != nil {
		child, exists := object[parts[0]]
		if !exists && (remove || len(parts) > 1) {
			return nil, fmt.Errorf("destination value missing")
		}
		updated, err := updateValue(child, parts[1:], value, remove)
		if err != nil {
			return nil, err
		}
		if updated == nil {
			delete(object, parts[0])
		} else {
			object[parts[0]] = updated
		}
		return json.Marshal(object)
	}
	var array []json.RawMessage
	if json.Unmarshal(raw, &array) == nil && array != nil {
		index, err := strconv.Atoi(parts[0])
		if err != nil || index < 0 || index >= len(array) || strconv.Itoa(index) != parts[0] {
			return nil, fmt.Errorf("array destination missing")
		}
		updated, err := updateValue(array[index], parts[1:], value, remove)
		if err != nil {
			return nil, err
		}
		if updated == nil {
			array = append(array[:index], array[index+1:]...)
		} else {
			array[index] = updated
		}
		return json.Marshal(array)
	}
	return nil, fmt.Errorf("destination must be an object or array")
}
