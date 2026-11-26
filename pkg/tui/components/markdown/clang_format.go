package markdown

import (
	"context"
	_ "embed"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

//go:embed clang-format/plugin.wasm
var clangFormatWASM []byte

// Keep the compiled module for the process lifetime. Each snippet gets its own
// instance so guest memory and configuration are never shared across renders.
type compiledClangFormat struct {
	runtime wazero.Runtime
	module  wazero.CompiledModule
}

var clangFormatModule atomic.Pointer[compiledClangFormat]

// WarmClangFormat compiles the formatter once. Call outside the render loop.
// Until it succeeds, Render displays the original C and C++ source.
func WarmClangFormat() bool {
	return warmClangFormat()
}

var warmClangFormat = sync.OnceValue(func() bool {
	ctx := context.Background()
	const memoryPages = 1024 // 64 MiB per formatting instance.
	config := wazero.NewRuntimeConfig().WithMemoryLimitPages(memoryPages).WithCloseOnContextDone(true)
	runtime := wazero.NewRuntimeWithConfig(ctx, config)
	module, err := runtime.CompileModule(ctx, clangFormatWASM)
	if err != nil {
		_ = runtime.Close(ctx) // The formatter falls back to the original text.
		return false
	}
	clangFormatModule.Store(&compiledClangFormat{runtime: runtime, module: module})
	return true
})

func formatClang(source, filename string) string {
	compiled := clangFormatModule.Load()
	if compiled == nil {
		return source
	}

	const timeout = 200 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	module, err := compiled.runtime.InstantiateModule(ctx, compiled.module, wazero.NewModuleConfig().WithName(""))
	if err != nil {
		return source
	}
	defer module.Close(context.Background()) // The instance owns no host resources.

	const configID = 1
	const config = `{"global":{"lineWidth":100,"indentWidth":2},"plugin":{"SortIncludes":"Never"}}`
	if !writeClangInput(ctx, module, config) {
		return source
	}
	_, err = module.ExportedFunction("register_config").Call(ctx, configID)
	if err != nil || !writeClangInput(ctx, module, filename) {
		return source
	}
	_, err = module.ExportedFunction("set_file_path").Call(ctx)
	if err != nil || !writeClangInput(ctx, module, source) {
		return source
	}

	status, err := module.ExportedFunction("format").Call(ctx, configID)
	const changed = 1
	if err != nil || status[0] != changed {
		return source
	}
	size, err := module.ExportedFunction("get_formatted_text").Call(ctx)
	if err != nil || size[0] > maxCodeBytes {
		return source
	}
	pointer, err := module.ExportedFunction("get_shared_bytes_ptr").Call(ctx)
	if err != nil {
		return source
	}

	formatted, ok := module.Memory().Read(uint32(pointer[0]), uint32(size[0]))
	if !ok || !utf8.Valid(formatted) {
		return source
	}
	return string(formatted)
}

func writeClangInput(ctx context.Context, module api.Module, source string) bool {
	pointer, err := module.ExportedFunction("clear_shared_bytes").Call(ctx, uint64(len(source)))
	return err == nil && module.Memory().WriteString(uint32(pointer[0]), source)
}
