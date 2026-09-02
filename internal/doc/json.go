package doc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// Reserved keys cannot be used as field names, because a node writes its
// fields at the top level alongside them.
var reserved = map[string]bool{
	"id": true, "type": true, "children": true, "links": true, "fields": true,
	"items": true, "page": true, "columns": true, "rows": true,
}

// active is the registry used to order fields when writing a node. There is
// exactly one registry per process, set by LoadTypes; if it is unset, fields
// fall back to alphabetical order so output stays deterministic either way.
var active *Registry

// MarshalJSON writes a node flat: id, type, then its fields in the order they
// are declared in types.json, then links and children. Declared order (rather
// than Go's map ordering, which sorts) keeps diffs stable and readable, which
// matters when the files are the history.
func (n *Node) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')

	first := true
	put := func(key string, v any) error {
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("field %q: %w", key, err)
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		k, _ := json.Marshal(key)
		b.Write(k)
		b.WriteByte(':')
		b.Write(raw)
		return nil
	}

	if err := put("id", n.ID); err != nil {
		return nil, err
	}
	if err := put("type", n.Type); err != nil {
		return nil, err
	}

	written := map[string]bool{}
	if active != nil {
		if td := active.Get(n.Type); td != nil {
			for _, fd := range td.Fields {
				v, ok := n.Fields[fd.Name]
				if !ok {
					continue
				}
				if err := put(fd.Name, v); err != nil {
					return nil, err
				}
				written[fd.Name] = true
			}
		}
	}

	// Anything left over — a field from a type that has since changed — is
	// kept rather than dropped, so an edit never silently loses data.
	var rest []string
	for k := range n.Fields {
		if !written[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		if err := put(k, n.Fields[k]); err != nil {
			return nil, err
		}
	}

	if n.Page != "" {
		if err := put("page", n.Page); err != nil {
			return nil, err
		}
	}
	// A table's shape, written before its content the way a header row is read
	// before the rows under it.
	if len(n.Columns) > 0 {
		if err := put("columns", n.Columns); err != nil {
			return nil, err
		}
	}
	if len(n.Rows) > 0 {
		if err := put("rows", n.Rows); err != nil {
			return nil, err
		}
	}
	if len(n.Links) > 0 {
		if err := put("links", n.Links); err != nil {
			return nil, err
		}
	}
	if len(n.Items) > 0 {
		raw, err := marshalItems(n, n.Type)
		if err != nil {
			return nil, err
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(`"items":`)
		b.Write(raw)
	}
	if len(n.Children) > 0 {
		if err := put("children", n.Children); err != nil {
			return nil, err
		}
	}

	b.WriteByte('}')
	return b.Bytes(), nil
}

// marshalItems writes a line's sub-lines. They carry no "type" — it is their
// parent's — and no children, so writing them through the normal node
// marshaller would add keys that mean nothing here. Sub-lines of their own
// they may have, and those are written the same way.
func marshalItems(parent *Node, typeName string) ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, it := range parent.Items {
		if i > 0 {
			b.WriteByte(',')
		}
		raw, err := marshalItem(it, typeName)
		if err != nil {
			return nil, err
		}
		b.Write(raw)
	}
	b.WriteByte(']')
	return b.Bytes(), nil
}

func marshalItem(n *Node, parentType string) ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')

	first := true
	put := func(key string, v any) error {
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("item field %q: %w", key, err)
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		k, _ := json.Marshal(key)
		b.Write(k)
		b.WriteByte(':')
		b.Write(raw)
		return nil
	}

	if err := put("id", n.ID); err != nil {
		return nil, err
	}
	written := map[string]bool{}
	if active != nil {
		if td := active.Get(parentType); td != nil {
			for _, fd := range td.Fields {
				if v, ok := n.Fields[fd.Name]; ok {
					if err := put(fd.Name, v); err != nil {
						return nil, err
					}
					written[fd.Name] = true
				}
			}
		}
	}
	var rest []string
	for k := range n.Fields {
		if !written[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		if err := put(k, n.Fields[k]); err != nil {
			return nil, err
		}
	}
	if len(n.Links) > 0 {
		if err := put("links", n.Links); err != nil {
			return nil, err
		}
	}
	if len(n.Items) > 0 {
		raw, err := marshalItems(n, parentType)
		if err != nil {
			return nil, err
		}
		if !first {
			b.WriteByte(',')
		}
		b.WriteString(`"items":`)
		b.Write(raw)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// UnmarshalJSON reads the flat form, and still accepts the older nested
// {"fields": {…}} shape so existing files load without migration.
func (n *Node) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	n.Fields = map[string]any{}
	for k, v := range raw {
		var err error
		switch k {
		case "id":
			err = json.Unmarshal(v, &n.ID)
		case "type":
			err = json.Unmarshal(v, &n.Type)
		case "children":
			err = json.Unmarshal(v, &n.Children)
		case "items":
			err = json.Unmarshal(v, &n.Items)
		case "page":
			err = json.Unmarshal(v, &n.Page)
		case "columns":
			err = json.Unmarshal(v, &n.Columns)
		case "rows":
			err = json.Unmarshal(v, &n.Rows)
		case "links":
			err = json.Unmarshal(v, &n.Links)
		case "fields":
			var old map[string]any
			if err = json.Unmarshal(v, &old); err == nil {
				for fk, fv := range old {
					n.Fields[fk] = fv
				}
			}
		default:
			var val any
			if err = json.Unmarshal(v, &val); err == nil {
				n.Fields[k] = val
			}
		}
		if err != nil {
			return fmt.Errorf("key %q: %w", k, err)
		}
	}
	return nil
}
