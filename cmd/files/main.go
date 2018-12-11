package main

import (
	"fmt"
	"log"
	"os"

	"go.starlark.net/starlark"
)

var (
	ErrArgsNotSupported = fmt.Errorf("Positional argument is not supported")
)

func main() {
	name := os.Args[1]

	thread := &starlark.Thread{}

	ws := &Workspace{}

	initdef := starlark.StringDict{
		"rule": starlark.NewBuiltin("rule", ws.rule),
		"attr": &attr{},
	}

	src := `
module = rule(
    attrs = dict(
        srcs = attr.files(),
        deps = attr.modules(),
    ),
)

def empty(name):
    module(name=name, srcs=[], deps=[])
`
	predeclared, err := starlark.ExecFile(thread, "", src, initdef)
	log.Printf("init: err=%v", err)
	log.Printf("init: globals=%+v", predeclared)

	filename := "MODS"
	_, err = starlark.ExecFile(thread, filename, nil, predeclared)
	if err != nil {
		if evalErr, ok := err.(*starlark.EvalError); ok {
			log.Fatal(evalErr.Backtrace())
		}
		log.Fatal(err)
		os.Exit(1)
	}

	files, err := ws.GetFiles("MODS", name)
	if err != nil {
		log.Printf("error: %s", err)
	} else {
		for _, file := range files {
			log.Printf("file: %s", file)
		}
	}
}

type Workspace struct {
	modules map[[2]string]Module
}

type Module struct {
	fname      string
	name       string
	filedeps   map[string]filedep
	moduledeps map[string]moduledep
}

func (ws *Workspace) RegisterModule(fname, name string, filedeps map[string]filedep, moduledeps map[string]moduledep) {
	if ws.modules == nil {
		ws.modules = make(map[[2]string]Module)
	}

	log.Printf("ws: register module: fname=%s name=%s files=%s modules=%s", fname, name, filedeps, moduledeps)
	key := [2]string{fname, name}
	mod := Module{
		fname:      fname,
		name:       name,
		filedeps:   filedeps,
		moduledeps: moduledeps,
	}
	ws.modules[key] = mod
}

func (ws *Workspace) GetFiles(fname, name string) ([]string, error) {
	key := [2]string{fname, name}
	mod, ok := ws.modules[key]
	if !ok {
		return []string{}, fmt.Errorf("Unable to find module")
	}

	files := make([]string, 0)
	for _, fd := range mod.filedeps {
		for _, file := range fd.items {
			files = append(files, file)
		}
	}
	return files, nil
}

func (ws *Workspace) rule(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	r := ruleimpl{
		ws:    ws,
		attrs: make(map[string]starlark.Value),
		types: make([]string, 0),
	}

	for _, kwarg := range kwargs {
		key := kwarg[0].(starlark.String).GoString()
		if key == "attrs" {
			attrs := kwarg[1].(*starlark.Dict)
			for _, kv := range attrs.Items() {
				attr := kv[0].(starlark.String).GoString()
				r.attrs[attr] = kv[1]
			}
		} else if key == "type" {
			r.types, _ = toStrings(kwarg[1])
		} else {
			return nil, fmt.Errorf("Unexpected key: %s", key)
		}
	}

	return starlark.NewBuiltin("ruleimpl", r.fn), nil
}

type filedep struct {
	items []string
}

type moduledep struct {
	types []string
	items []string
}

type ruleimpl struct {
	ws    *Workspace
	attrs map[string]starlark.Value
	types []string
}

func (r ruleimpl) fn(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	filedeps := make(map[string]filedep)
	moduledeps := make(map[string]moduledep)

	for _, kwarg := range kwargs {
		key := kwarg[0].(starlark.String).GoString()
		if key == "name" {
			name = kwarg[1].(starlark.String).GoString()
		} else {
			attr, ok := r.attrs[key]
			if !ok {
				return nil, fmt.Errorf("Unexpected key: %s", key)
			}

			items, err := toStrings(kwarg[1])
			if err != nil {
				return nil, err
			}

			if _, ok := attr.(files); ok {
				filedeps[key] = filedep{items}
			} else if mods, ok := attr.(modules); ok {
				moduledeps[key] = moduledep{mods.types, items}
			}

		}
	}

	if name == "" {
		return nil, fmt.Errorf("name parameter is required")
	}

	parent := thread.Caller()
	for parent != nil {
		log.Printf("-- frame: %+v, fname: %s", parent, parent.Position().Filename())
		parent = parent.Parent()
	}

	fname := thread.Caller().Position().Filename()

	log.Printf("Register module: fname=%s name=%s files=%+v modules=%+v", fname, name, filedeps, moduledeps)

	r.ws.RegisterModule(fname, name, filedeps, moduledeps)

	return starlark.None, nil
}

func toStrings(v starlark.Value) ([]string, error) {
	list, ok := v.(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("Not a list")
	}

	var values []string
	for i := 0; i < list.Len(); i += 1 {
		item := list.Index(i)
		str, ok := item.(starlark.String)
		if !ok {
			return nil, fmt.Errorf("Not a string list")
		}
		values = append(values, str.GoString())
	}

	return values, nil
}

/*

to be implemented:

native:
  glob
  [x] module

libs' rule
  [x] rule
  attr:
	[x] files
	[x] modules

*/

var (
	_ starlark.HasAttrs = &attr{}
)

type attr struct {
}

func (a *attr) String() string        { return "attr" }
func (a *attr) Type() string          { return "attr" }
func (a *attr) Freeze()               {}
func (a *attr) Truth() starlark.Bool  { return starlark.True }
func (a *attr) Hash() (uint32, error) { return 0, nil }

func (a *attr) AttrNames() []string { return []string{"files", "modules"} }

func (a *attr) Attr(name string) (starlark.Value, error) {
	if name == "files" {
		return starlark.NewBuiltin("files", attrFiles), nil
	} else if name == "modules" {
		return starlark.NewBuiltin("modules", attrModules), nil
	} else {
		return nil, fmt.Errorf("Undefined attribute: %s", name)
	}
}

func attrFiles(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return files{}, nil
}

func attrModules(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var types []string
	for _, kwarg := range kwargs {
		key := kwarg[0].(starlark.String).GoString()
		if key == "types" {
			values, ok := kwarg[1].(*starlark.List)
			if !ok {
				return nil, fmt.Errorf("Expected list")
			}

			size := values.Len()
			for i := 0; i < size; i += 1 {
				value := values.Index(i)
				v, ok := value.(starlark.String)
				if !ok {
					return nil, fmt.Errorf("Expected string")
				}
				types = append(types, v.GoString())
			}
		} else {
			return nil, fmt.Errorf("Unexpected key: %s", key)
		}
	}

	return modules{types}, nil
}

type files struct {
}

func (files) String() string        { return "files" }
func (files) Type() string          { return "files" }
func (files) Freeze()               {}
func (files) Truth() starlark.Bool  { return starlark.True }
func (files) Hash() (uint32, error) { return 0, nil }

type modules struct {
	types []string
}

func (m modules) String() string      { return fmt.Sprintf("modules: types=%s", m.types) }
func (modules) Type() string          { return "modules" }
func (modules) Freeze()               {}
func (modules) Truth() starlark.Bool  { return starlark.True }
func (modules) Hash() (uint32, error) { return 0, nil }
