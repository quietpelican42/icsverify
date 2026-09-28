package ics

import "fmt"

// Property is a parsed content line attached to a Component.
type Property struct {
	Name   string
	Params []Param
	Value  string
	Line   int
}

func (p Property) cl() ContentLine {
	return ContentLine{Name: p.Name, Params: p.Params, Value: p.Value, Line: p.Line}
}

func (p Property) Param(name string) (values []string, ok bool) {
	return p.cl().Param(name)
}

func (p Property) ParamValue(name string) string {
	return p.cl().ParamValue(name)
}

// Component is a BEGIN:X/END:X block: VCALENDAR, VEVENT, VTIMEZONE, STANDARD,
// and so on (RFC 5545 §3.6).
type Component struct {
	Name       string
	Properties []Property
	Children   []*Component
	Line       int // line of the BEGIN property
	Parent     *Component
	// index among siblings of the same Name, used to build Path.
	index int
}

// Path renders a component's position for diagnostics, e.g.
// "VCALENDAR/VEVENT[1]".
func (c *Component) Path() string {
	if c.Parent == nil {
		return c.Name
	}
	return fmt.Sprintf("%s/%s[%d]", c.Parent.Path(), c.Name, c.index)
}

// All returns the properties named n (case-insensitive), in document order.
func (c *Component) All(n string) []Property {
	var out []Property
	for _, p := range c.Properties {
		if equalFold(p.Name, n) {
			out = append(out, p)
		}
	}
	return out
}

// First returns the first property named n and whether it was present.
func (c *Component) First(n string) (Property, bool) {
	for _, p := range c.Properties {
		if equalFold(p.Name, n) {
			return p, true
		}
	}
	return Property{}, false
}

// Has reports whether property n is present at least once.
func (c *Component) Has(n string) bool {
	_, ok := c.First(n)
	return ok
}

// ChildrenNamed returns direct child components with the given name.
func (c *Component) ChildrenNamed(n string) []*Component {
	var out []*Component
	for _, ch := range c.Children {
		if equalFold(ch.Name, n) {
			out = append(out, ch)
		}
	}
	return out
}

// buildTree consumes a flat list of ContentLines (already unfolded and
// tokenized) and builds the BEGIN/END component tree. Structural problems
// (unmatched END, mismatched name, content after all components close,
// content before any BEGIN) are reported into res; parsing continues on a
// best-effort basis so the rest of the file is still checked.
//
// ok is false only when no usable VCALENDAR component tree could be
// produced at all (caller should treat this as "could not analyse").
func buildTree(lines []ContentLine, res *Result) (root *Component, ok bool) {
	nameCounts := map[*Component]map[string]int{}
	var stack []*Component
	var top *Component // first top-level VCALENDAR found, if any

	nextIndex := func(parent *Component, name string) int {
		m := nameCounts[parent]
		if m == nil {
			m = map[string]int{}
			nameCounts[parent] = m
		}
		idx := m[name]
		m[name]++
		return idx
	}

	for _, cl := range lines {
		switch {
		case equalFold(cl.Name, "BEGIN"):
			var parent *Component
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			comp := &Component{Name: cl.Value, Line: cl.Line, Parent: parent}
			comp.index = nextIndex(parent, comp.Name)
			if parent != nil {
				parent.Children = append(parent.Children, comp)
			} else if top == nil && equalFold(comp.Name, "VCALENDAR") {
				top = comp
			} else if top == nil {
				res.errorf("content-before-vcalendar", "RFC 5545 §3.4", "", cl.Line,
					"BEGIN:%s appears outside any VCALENDAR object", comp.Name)
			} else {
				res.errorf("multiple-top-level-objects", "RFC 5545 §3.4", "", cl.Line,
					"a second top-level BEGIN:%s appears after the first VCALENDAR object; each is validated as if separate is not supported by this tool", comp.Name)
			}
			stack = append(stack, comp)

		case equalFold(cl.Name, "END"):
			if len(stack) == 0 {
				res.errorf("unmatched-end", "RFC 5545 §3.1", "", cl.Line,
					"END:%s has no matching BEGIN", cl.Value)
				continue
			}
			curr := stack[len(stack)-1]
			if !equalFold(curr.Name, cl.Value) {
				res.errorf("mismatched-end", "RFC 5545 §3.1", curr.Path(), cl.Line,
					"END:%s does not match BEGIN:%s", cl.Value, curr.Name)
				// Don't pop: assume the END was a typo and keep waiting
				// for the real one, so nested content isn't misattributed.
				continue
			}
			stack = stack[:len(stack)-1]

		default:
			if len(stack) == 0 {
				res.errorf("content-before-vcalendar", "RFC 5545 §3.4", "", cl.Line,
					"property %s appears before any BEGIN:VCALENDAR", cl.Name)
				continue
			}
			curr := stack[len(stack)-1]
			curr.Properties = append(curr.Properties, Property{
				Name: cl.Name, Params: cl.Params, Value: cl.Value, Line: cl.Line,
			})
		}
	}

	for _, unclosed := range stack {
		res.errorf("unclosed-component", "RFC 5545 §3.1", unclosed.Path(), unclosed.Line,
			"BEGIN:%s is never closed with a matching END", unclosed.Name)
	}

	if top == nil {
		return nil, false
	}
	return top, true
}
