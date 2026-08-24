package doc

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
)

//go:embed types.json
var defaultTypes []byte

// FieldDef is one editable field on a node type. Kind drives both the editor
// control and how @-queries interpret the value.
//
// Kinds: richtext, text, slug, number, bool, tag, url.
type FieldDef struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Default     any    `json:"default,omitempty"`
}

// TypeDef is a line template: which fields a line of this type has, whether it
// can nest, and what it looks like in the gutter.
type TypeDef struct {
	Name string `json:"name"`
	// Label is the name shown in the type picker.
	Label string `json:"label"`
	// Icon is the single glyph shown in the gutter.
	Icon string `json:"icon"`
	// Nestable reports whether this type can have children at all.
	Nestable bool `json:"nestable"`
	// AllowsHeaders reports whether those children may be headers. Only
	// headers carry the outline, so lists and todos nest but stay leaves.
	AllowsHeaders bool `json:"allowsHeaders"`
	// Primary is the field the caret lands in and that Enter/Tab operate on.
	Primary string `json:"primary"`
	// Continues reports whether pressing Enter creates another line of the
	// same type (true for todos and list items) or a plain text line.
	Continues bool       `json:"continues"`
	Fields    []FieldDef `json:"fields"`
}

// Field returns the definition of one field by name.
func (t *TypeDef) Field(name string) *FieldDef {
	for i := range t.Fields {
		if t.Fields[i].Name == name {
			return &t.Fields[i]
		}
	}
	return nil
}

// Registry is the set of line types, in picker order.
type Registry struct {
	Types  []*TypeDef `json:"types"`
	Source string     `json:"-"` // where these were loaded from
	byName map[string]*TypeDef
}

// LoadTypes reads the type templates from path, falling back to the built-in
// defaults when the file does not exist. Editing that file is how you change
// the templates, exactly as the spec asks.
func LoadTypes(path string) (*Registry, error) {
	raw, source := defaultTypes, "innebygd"
	if path != "" {
		b, err := os.ReadFile(path)
		if err == nil {
			raw, source = b, path
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
	}

	var reg Registry
	if err := json.Unmarshal(raw, &reg); err != nil {
		return nil, fmt.Errorf("parse types (%s): %w", source, err)
	}
	reg.Source = source
	reg.byName = make(map[string]*TypeDef, len(reg.Types))
	for _, t := range reg.Types {
		if t.Primary == "" && len(t.Fields) > 0 {
			t.Primary = t.Fields[0].Name
		}
		// Fields are written at the top level of a node, so they must not
		// collide with the keys the node itself owns.
		for _, fd := range t.Fields {
			if reserved[fd.Name] {
				return nil, fmt.Errorf("types (%s): type %q cannot have a field named %q — that name is reserved", source, t.Name, fd.Name)
			}
		}
		reg.byName[t.Name] = t
	}
	if reg.byName["header"] == nil || reg.byName["text"] == nil {
		return nil, fmt.Errorf("types (%s): the header and text types are required", source)
	}

	// One registry per process: node marshalling consults it for field order.
	active = &reg
	return &reg, nil
}

// Get returns a type by name, or nil.
func (r *Registry) Get(name string) *TypeDef {
	return r.byName[name]
}

// CanContain reports whether a node of parentType may hold a child of
// childType. An empty parentType means the top level of a page, which holds
// anything.
func (r *Registry) CanContain(parentType, childType string) bool {
	if parentType == "" {
		return true
	}
	p := r.Get(parentType)
	if p == nil || !p.Nestable {
		return false
	}
	if childType == "header" {
		return p.AllowsHeaders
	}
	return true
}

// Defaults returns a fresh field map for a new node of the given type.
func (r *Registry) Defaults(typeName string) map[string]any {
	f := map[string]any{}
	t := r.Get(typeName)
	if t == nil {
		return f
	}
	for _, fd := range t.Fields {
		switch {
		case fd.Default != nil:
			f[fd.Name] = fd.Default
		case fd.Kind == "bool":
			f[fd.Name] = false
		case fd.Kind == "number":
			f[fd.Name] = float64(0)
		default:
			f[fd.Name] = ""
		}
	}
	return f
}

// JSON returns the registry as JSON for the editor to consume.
func (r *Registry) JSON() (string, error) {
	b, err := json.Marshal(r)
	return string(b), err
}
