//go:build treesitter && cgo && (linux || darwin)

package highlight

/*
#cgo linux LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>
#include <string.h>
static void *micro_parser_open(const char *path, char **error) {
 void *handle = dlopen(path, RTLD_NOW | RTLD_LOCAL);
 if (!handle) { const char *message = dlerror(); *error = strdup(message ? message : "unknown loader error"); }
 return handle;
}
static void *micro_parser_language(void *handle, const char *symbol) {
 void *address = dlsym(handle, symbol);
 if (!address) return NULL;
 return (void *)((const void *(*)(void))address)();
}
*/
import "C"

import (
	"fmt"
	ts "github.com/tree-sitter/go-tree-sitter"
	"sync"
	"unsafe"
)

// Libraries are pinned until process exit. Never unload grammar code while a
// parser/tree might still reference it. Replacing the .so requires a restart.
var nativeParsers = struct {
	sync.Mutex
	handles map[string]unsafe.Pointer
}{handles: make(map[string]unsafe.Pointer)}

func loadNativeLanguage(path, symbol string) (*ts.Language, error) {
	nativeParsers.Lock()
	defer nativeParsers.Unlock()
	handle := nativeParsers.handles[path]
	if handle == nil {
		cpath := C.CString(path)
		defer C.free(unsafe.Pointer(cpath))
		var loaderError *C.char
		handle = C.micro_parser_open(cpath, &loaderError)
		if loaderError != nil {
			defer C.free(unsafe.Pointer(loaderError))
		}
		if handle == nil {
			return nil, fmt.Errorf("load parser %s: %s", path, C.GoString(loaderError))
		}
		nativeParsers.handles[path] = handle
	}
	csymbol := C.CString(symbol)
	defer C.free(unsafe.Pointer(csymbol))
	language := C.micro_parser_language(handle, csymbol)
	if language == nil {
		return nil, fmt.Errorf("parser %s has no usable entry point %s", path, symbol)
	}
	return ts.NewLanguage(language), nil
}
