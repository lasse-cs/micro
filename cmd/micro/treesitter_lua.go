package main

import (
	"fmt"

	"github.com/micro-editor/micro/v2/internal/buffer"
	ulua "github.com/micro-editor/micro/v2/internal/lua"
	"github.com/micro-editor/micro/v2/pkg/highlight"
	lua "github.com/yuin/gopher-lua"
)

// The API is available even in regex-only builds, where registration returns a
// clear error. Plugins never need to guess whether a native backend was compiled.
func luaImportMicroTreeSitter() *lua.LTable {
	L := ulua.L
	pkg := L.NewTable()
	L.SetField(pkg, "RegisterLanguage", L.NewFunction(func(L *lua.LState) int {
		spec, err := treeSitterSpecFromLua(L.CheckString(1), L.CheckTable(2))
		if err == nil {
			err = highlight.RegisterTreeSitterLanguage(spec)
		}
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		// Refresh existing shared documents once, including ones that used fallback.
		seen := make(map[*buffer.SharedBuffer]bool)
		for _, b := range buffer.OpenBuffers {
			if !seen[b.SharedBuffer] {
				seen[b.SharedBuffer] = true
				b.UpdateRules()
			}
		}
		L.Push(lua.LTrue)
		L.Push(lua.LNil)
		return 2
	}))
	L.SetField(pkg, "Languages", L.NewFunction(func(L *lua.LState) int {
		result := L.NewTable()
		for _, name := range highlight.TreeSitterLanguages() {
			result.Append(lua.LString(name))
		}
		L.Push(result)
		return 1
	}))
	return pkg
}

func treeSitterSpecFromLua(name string, options *lua.LTable) (highlight.TreeSitterLanguageSpec, error) {
	spec := highlight.TreeSitterLanguageSpec{Name: name, Captures: make(map[string]string)}
	var problem error
	allowed := map[string]bool{"parser": true, "symbol": true, "highlights": true, "filetypes": true, "captures": true}
	options.ForEach(func(k, v lua.LValue) {
		if !allowed[k.String()] {
			problem = fmt.Errorf("unknown language option %s", k.String())
		}
	})
	if problem != nil {
		return spec, problem
	}
	field := func(key string, optional bool) (string, error) {
		value := options.RawGetString(key)
		if optional && value == lua.LNil {
			return "", nil
		}
		str, ok := value.(lua.LString)
		if !ok || (!optional && str == "") {
			return "", fmt.Errorf("%s must be a nonempty string", key)
		}
		return string(str), nil
	}
	var err error
	if spec.Parser, err = field("parser", false); err != nil {
		return spec, err
	}
	if spec.Highlights, err = field("highlights", false); err != nil {
		return spec, err
	}
	if spec.Symbol, err = field("symbol", true); err != nil {
		return spec, err
	}
	types, ok := options.RawGetString("filetypes").(*lua.LTable)
	if !ok || types.Len() == 0 {
		return spec, fmt.Errorf("filetypes must be a nonempty array of strings")
	}
	types.ForEach(func(k, v lua.LValue) {
		number, valid := k.(lua.LNumber)
		str, isString := v.(lua.LString)
		if !valid || int(number) < 1 || int(number) > types.Len() || lua.LNumber(int(number)) != number || !isString || str == "" {
			problem = fmt.Errorf("filetypes must be a dense array of nonempty strings")
		}
	})
	if problem != nil {
		return spec, problem
	}
	for i := 1; i <= types.Len(); i++ {
		str, ok := types.RawGetInt(i).(lua.LString)
		if !ok {
			return spec, fmt.Errorf("filetypes must not contain holes")
		}
		spec.Filetypes = append(spec.Filetypes, string(str))
	}
	if value := options.RawGetString("captures"); value != lua.LNil {
		captures, ok := value.(*lua.LTable)
		if !ok {
			return spec, fmt.Errorf("captures must be a string-to-string table")
		}
		captures.ForEach(func(k, v lua.LValue) {
			key, kok := k.(lua.LString)
			value, vok := v.(lua.LString)
			if !kok || !vok {
				problem = fmt.Errorf("captures must map strings to strings")
				return
			}
			spec.Captures[string(key)] = string(value)
		})
	}
	return spec, problem
}
