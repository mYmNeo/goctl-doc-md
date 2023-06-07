package main

import (
	"bytes"
	"flag"
	"fmt"
	"html/template"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/zeromicro/go-zero/core/stringx"
	"github.com/zeromicro/go-zero/tools/goctl/api/spec"
	apiutil "github.com/zeromicro/go-zero/tools/goctl/api/util"
	"github.com/zeromicro/go-zero/tools/goctl/plugin"
	"github.com/zeromicro/go-zero/tools/goctl/util"
)

var templateFile = flag.String("template", "", "the config file")

func main() {
	flag.Parse()

	if len(*templateFile) == 0 {
		fmt.Println("missing -template")
		os.Exit(1)
	}

	p, err := plugin.NewPlugin()
	if err != nil {
		panic(err)
	}

	f, err := os.Open(*templateFile)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	templateBytes, err := io.ReadAll(f)
	if err != nil {
		panic(err)
	}

	api := p.Api
	allTypes := make(map[string]spec.Type)
	for i := range api.Types {
		allTypes[api.Types[i].Name()] = api.Types[i]
	}

	var builder strings.Builder
	for index, route := range api.Service.Routes() {
		inputData := make(map[string]string)

		var title string
		var routeComments []string
		for k, v := range route.AtDoc.Properties {
			if k == "title" {
				title = v
				continue
			}
			routeComments = append(routeComments, fmt.Sprintf("%s: %s", k, strings.TrimSpace(v)))
		}

		requestContent, err := buildDoc(route.RequestType, allTypes)
		if err != nil {
			panic(err)
		}

		responseContent, err := buildDoc(route.ResponseType, allTypes)
		if err != nil {
			panic(err)
		}

		appendData(inputData, "index", strconv.Itoa(index+1))
		appendData(inputData, "title", title)
		appendData(inputData, "routeComment", strings.Join(routeComments, "\n\n"))
		appendData(inputData, "method", strings.ToUpper(route.Method))
		appendData(inputData, "uri", route.Path)
		appendData(inputData, "requestType", "`"+stringx.TakeOne(route.RequestTypeName(), "-")+"`")
		appendData(inputData, "responseType", "`"+stringx.TakeOne(route.ResponseTypeName(), "-")+"`")
		appendData(inputData, "requestContent", requestContent)
		appendData(inputData, "responseContent", responseContent)

		t := template.Must(template.New("markdownTemplate").Parse(string(templateBytes)))
		var tmplBytes bytes.Buffer
		err = t.Execute(&tmplBytes, inputData)

		if err != nil {
			panic(err)
		}
		builder.Write(tmplBytes.Bytes())
	}

	_, err = os.Stdout.WriteString(strings.Replace(builder.String(), "&#34;", `"`, -1))
}

func appendData(data map[string]string, key, value string) {
	data[key] = value
}

func isValidRoute(route spec.Type) bool {
	if route == nil || len(route.Name()) == 0 {
		return false
	}
	return true
}

func buildDoc(route spec.Type, allTypes map[string]spec.Type) (string, error) {
	if !isValidRoute(route) {
		return "", nil
	}

	tps := make(map[string]spec.Type)
	tps[route.Name()] = route
	if _, ok := route.(spec.DefineStruct); ok {
		associatedTypes(route, tps, allTypes)
	}
	value, err := buildTypes(tps, allTypes)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("\n\n```golang\n%s\n```\n", value), nil
}

func associatedTypes(tp spec.Type, tps map[string]spec.Type, allTypes map[string]spec.Type) {
	var (
		name, searchName string
	)

	name = tp.Name()
	if len(name) > 0 && name[0] == '*' {
		searchName = name[1:]
	} else {
		searchName = name
	}

	_, hasAdded := tps[name]
	savedType, saved := allTypes[searchName]
	if !hasAdded && saved {
		tps[name] = savedType
	}

	switch v := savedType.(type) {
	case spec.PointerType:
		associatedTypes(v.Type, tps, allTypes)
	default:
	}

	if definedType, ok := savedType.(spec.DefineStruct); ok {
		for _, item := range definedType.Members {
			switch v := item.Type.(type) {
			case spec.DefineStruct:
				associatedTypes(item.Type, tps, allTypes)
			case spec.MapType:
				if _, pt := v.Value.(spec.PrimitiveType); !pt {
					associatedTypes(v.Value, tps, allTypes)
				}
			case spec.ArrayType:
				if _, pt := v.Value.(spec.PrimitiveType); !pt {
					associatedTypes(v.Value, tps, allTypes)
				}
			case spec.PointerType:
				associatedTypes(v, tps, allTypes)
			default:
			}
		}
	}
}

// buildTypes gen types to string
func buildTypes(types map[string]spec.Type, all map[string]spec.Type) (string, error) {
	var builder strings.Builder
	first := true
	for _, tp := range types {
		if first {
			first = false
		} else {
			builder.WriteString("\n\n")
		}

		if err := writeType(&builder, tp, all); err != nil {
			return "", apiutil.WrapErr(err, "Type "+tp.Name()+" generate error")
		}
	}

	return builder.String(), nil
}

func writeType(writer io.Writer, tp spec.Type, all map[string]spec.Type) error {
	fmt.Fprintf(writer, "type %s struct {\n", util.Title(tp.Name()))
	if err := writerMembers(writer, tp, all); err != nil {
		return err
	}
	fmt.Fprintf(writer, "}")
	return nil
}

func writerMembers(writer io.Writer, tp spec.Type, all map[string]spec.Type) error {
	structType, ok := tp.(spec.DefineStruct)
	if !ok {
		return fmt.Errorf("unspport struct type: %s", tp.Name())
	}

	for _, member := range structType.Members {
		if err := writeProperty(writer, member.Name, member.Tag, member.GetComment(), member.Type, 1); err != nil {
			return err
		}
	}

	return nil
}

func writeProperty(writer io.Writer, name, tag, comment string, tp spec.Type, indent int) error {
	apiutil.WriteIndent(writer, indent)
	var err error
	if len(comment) > 0 {
		comment = strings.TrimPrefix(comment, "//")
		comment = "//" + comment
		if len(name) > 0 {
			_, err = fmt.Fprintf(writer, "%s ", strings.Title(name))
		}
		_, err = fmt.Fprintf(writer, "%s %s %s\n", tp.Name(), tag, comment)
	} else {
		if len(name) > 0 {
			_, err = fmt.Fprintf(writer, "%s ", strings.Title(name))
		}
		_, err = fmt.Fprintf(writer, "%s %s\n", tp.Name(), tag)
	}

	return err
}
