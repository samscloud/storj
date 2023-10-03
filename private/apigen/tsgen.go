// Copyright (C) 2022 Storj Labs, Inc.
// See LICENSE for copying information.

package apigen

import (
	"fmt"
	"os"
	"strings"

	"github.com/zeebo/errs"
)

// MustWriteTS writes generated TypeScript code into a file.
// If an error occurs, it panics.
func (a *API) MustWriteTS(path string) {
	f := newTSGenFile(path, a)

	f.generateTS()

	err := f.write()
	if err != nil {
		panic(errs.Wrap(err))
	}
}

type tsGenFile struct {
	result string
	path   string
	api    *API
	types  Types
}

func newTSGenFile(filepath string, api *API) *tsGenFile {
	return &tsGenFile{
		path:  filepath,
		api:   api,
		types: NewTypes(),
	}
}

func (f *tsGenFile) pf(format string, a ...interface{}) {
	f.result += fmt.Sprintf(format+"\n", a...)
}

func (f *tsGenFile) write() error {
	content := strings.ReplaceAll(f.result, "\t", "    ")
	return os.WriteFile(f.path, []byte(content), 0644)
}

func (f *tsGenFile) generateTS() {
	f.pf("// AUTOGENERATED BY private/apigen")
	f.pf("// DO NOT EDIT.")
	f.pf("")
	f.pf("import { HttpClient } from '@/utils/httpClient';")

	f.registerTypes()
	f.result += f.types.GenerateTypescriptDefinitions()

	for _, group := range f.api.EndpointGroups {
		// Not sure if this is a good name
		f.createAPIClient(group)
	}
}

func (f *tsGenFile) registerTypes() {
	// TODO: what happen with path parameters?
	for _, group := range f.api.EndpointGroups {
		for _, method := range group.endpoints {
			if method.Request != nil {
				f.types.Register(method.requestType())
			}
			if method.Response != nil {
				f.types.Register(method.responseType())
			}
			if len(method.QueryParams) > 0 {
				for _, p := range method.QueryParams {
					// TODO: Is this call needed? this breaks the named type for slices and arrays and pointers.
					t := getElementaryType(p.namedType(method.Endpoint, "query"))
					f.types.Register(t)
				}
			}
		}
	}
}

func (f *tsGenFile) createAPIClient(group *EndpointGroup) {
	f.pf("\nexport class %sHttpApi%s {", capitalize(group.Prefix), strings.ToUpper(f.api.Version))
	f.pf("\tprivate readonly http: HttpClient = new HttpClient();")
	f.pf("\tprivate readonly ROOT_PATH: string = '%s/%s';", f.api.endpointBasePath(), group.Prefix)
	for _, method := range group.endpoints {
		f.pf("")

		funcArgs, path := f.getArgsAndPath(method)

		returnStmt := "return"
		returnType := "void"
		if method.Response != nil {
			respType := method.responseType()
			returnType = TypescriptTypeName(respType)
			returnStmt += fmt.Sprintf(" response.json().then((body) => body as %s)", returnType)
		}
		returnStmt += ";"

		f.pf("\tpublic async %s(%s): Promise<%s> {", method.TypeScriptName, funcArgs, returnType)
		if len(method.QueryParams) > 0 {
			f.pf("\t\tconst u = new URL(`%s`);", path)
			for _, p := range method.QueryParams {
				f.pf("\t\tu.searchParams.set('%s', %s);", p.Name, p.Name)
			}
			f.pf("\t\tconst fullPath = u.toString();")
		} else {
			f.pf("\t\tconst fullPath = `%s`;", path)
		}

		if method.Request != nil {
			f.pf("\t\tconst response = await this.http.%s(fullPath, JSON.stringify(request));", strings.ToLower(method.Method))
		} else {
			f.pf("\t\tconst response = await this.http.%s(fullPath);", strings.ToLower(method.Method))
		}

		f.pf("\t\tif (response.ok) {")
		f.pf("\t\t\t%s", returnStmt)
		f.pf("\t\t}")
		f.pf("\t\tconst err = await response.json();")
		f.pf("\t\tthrow new Error(err.error);")
		f.pf("\t}")
	}
	f.pf("}")
}

func (f *tsGenFile) getArgsAndPath(method *fullEndpoint) (funcArgs, path string) {
	// remove path parameter placeholders
	path = method.Path
	i := strings.Index(path, "{")
	if i > -1 {
		path = method.Path[:i]
	}
	path = "${this.ROOT_PATH}" + path

	if method.Request != nil {
		funcArgs += fmt.Sprintf("request: %s, ", TypescriptTypeName(method.requestType()))
	}

	for _, p := range method.PathParams {
		funcArgs += fmt.Sprintf("%s: %s, ", p.Name, TypescriptTypeName(p.namedType(method.Endpoint, "path")))
		path += fmt.Sprintf("/${%s}", p.Name)
	}

	for _, p := range method.QueryParams {
		funcArgs += fmt.Sprintf("%s: %s, ", p.Name, TypescriptTypeName(p.namedType(method.Endpoint, "query")))
	}

	path = strings.ReplaceAll(path, "//", "/")

	return strings.Trim(funcArgs, ", "), path
}
