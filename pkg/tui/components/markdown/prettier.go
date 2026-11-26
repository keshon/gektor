package markdown

import (
	"embed"
	"sync"
	"time"

	"github.com/dop251/goja"
)

//go:embed prettier/*
var prettierAssets embed.FS

func prettierBundle(name string) func() (*goja.Program, error) {
	return sync.OnceValues(func() (*goja.Program, error) {
		path := "prettier/" + name + ".js"
		source, err := prettierAssets.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return goja.Compile(path, string(source), false)
	})
}

var prettierStandalone = prettierBundle("standalone")
var prettierBabel = prettierBundle("babel")
var prettierEstree = prettierBundle("estree")
var prettierHTML = prettierBundle("html")
var prettierPostCSS = prettierBundle("postcss")
var prettierGraphQL = prettierBundle("graphql")
var prettierRust = prettierBundle("rust")

var prettierEntry = sync.OnceValues(func() (*goja.Program, error) {
	return goja.Compile("format code fence", `(function(source, parser, filepath) {
		return prettier.format(source, {
			parser, filepath, plugins: prettierPlugins,
			embeddedLanguageFormatting: "off"
		});
	})`, false)
})

func formatPrettier(source, parser, filename string, plugins ...func() (*goja.Program, error)) string {
	bundles := append([]func() (*goja.Program, error){prettierStandalone}, plugins...)
	bundles = append(bundles, prettierEntry)
	programs := make([]*goja.Program, 0, len(bundles))
	for _, bundle := range bundles {
		program, err := bundle()
		if err != nil {
			return source
		}
		programs = append(programs, program)
	}

	// A fresh runtime exposes no host APIs. Only the embedded formatter runs.
	vm := goja.New()
	vm.SetMaxCallStackSize(512)
	timer := time.AfterFunc(prettierTimeout, func() { vm.Interrupt("formatting deadline") })
	defer timer.Stop()

	var entry goja.Value
	for _, program := range programs {
		var err error
		entry, err = vm.RunProgram(program)
		if err != nil {
			return source
		}
	}
	format, ok := goja.AssertFunction(entry)
	if !ok {
		return source
	}

	result, err := format(goja.Undefined(), vm.ToValue(source), vm.ToValue(parser), vm.ToValue(filename))
	if err != nil {
		return source
	}
	promise, ok := result.Export().(*goja.Promise)
	if !ok || promise.State() != goja.PromiseStateFulfilled {
		return source
	}

	formatted, ok := promise.Result().Export().(string)
	if !ok || len(formatted) > maxCodeBytes {
		return source
	}
	return formatted
}
