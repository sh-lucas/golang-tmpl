package mug

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
)

// paramSources are the struct tags that fill input fields from the request
// instead of the JSON body: `path:"id"`, `query:"limit"`, `header:"X-Key"`
// and `cookie:"session"`. Such fields must also be tagged `json:"-"`, so the
// body can never set them.
var paramSources = []string{"path", "query", "header", "cookie"}

type param struct {
	index  int
	source string
	name   string
	kind   reflect.Kind
	bits   int
}

// key identifies the param in validation errors, e.g. "query.limit".
func (p param) key() string { return p.source + "." + p.name }

// paramsOf inspects T once, when the route is built, and panics on a
// misconfigured input so the mistake surfaces at startup, not per request.
// shortcut: only top-level fields are read (embedded structs are ignored), and
// repeated query keys keep the first value; extend when an endpoint needs it.
func paramsOf(t reflect.Type) []param {
	if t.Kind() != reflect.Struct {
		return nil
	}
	var params []param
	for i := range t.NumField() {
		field := t.Field(i)
		p, ok := paramOf(t, field)
		if !ok {
			continue
		}
		p.index = i
		params = append(params, p)
	}
	return params
}

func paramOf(t reflect.Type, field reflect.StructField) (param, bool) {
	var p param
	for _, source := range paramSources {
		name, ok := field.Tag.Lookup(source)
		if !ok {
			continue
		}
		if p.source != "" {
			panic(fmt.Sprintf("mug: %s.%s has both %q and %q tags", t, field.Name, p.source, source))
		}
		if name == "" {
			panic(fmt.Sprintf("mug: %s.%s has an empty %q tag", t, field.Name, source))
		}
		p.source, p.name = source, name
	}
	if p.source == "" {
		return p, false
	}
	if !field.IsExported() {
		panic(fmt.Sprintf("mug: %s.%s must be exported to receive %q", t, field.Name, p.key()))
	}
	if field.Tag.Get("json") != "-" {
		panic(fmt.Sprintf("mug: %s.%s must be tagged json:\"-\" so the body cannot set %q", t, field.Name, p.key()))
	}
	p.kind = field.Type.Kind()
	switch p.kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		p.bits = field.Type.Bits()
	case reflect.String, reflect.Bool:
	default:
		panic(fmt.Sprintf("mug: %s.%s has unsupported type %s for %q", t, field.Name, field.Type, p.key()))
	}
	return p, true
}

// readParams fills the param fields of input (a pointer to a struct). Missing
// or empty values leave the zero value; `validate:"required"` rejects them.
// It returns the keys of params whose value does not parse.
func readParams(r *http.Request, params []param, input reflect.Value) map[string]string {
	if len(params) == 0 {
		return nil
	}
	var query url.Values
	invalid := map[string]string{}
	for _, p := range params {
		var raw string
		switch p.source {
		case "path":
			raw = r.PathValue(p.name)
		case "query":
			if query == nil {
				query = r.URL.Query()
			}
			raw = query.Get(p.name)
		case "header":
			raw = r.Header.Get(p.name)
		case "cookie":
			if cookie, err := r.Cookie(p.name); err == nil {
				raw = cookie.Value
			}
		}
		if raw == "" {
			continue
		}
		if err := setParam(input.Field(p.index), p, raw); err != nil {
			invalid[p.key()] = p.kind.String()
		}
	}
	if len(invalid) == 0 {
		return nil
	}
	return invalid
}

func setParam(field reflect.Value, p param, raw string) error {
	switch p.kind {
	case reflect.String:
		field.SetString(raw)
	case reflect.Bool:
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		field.SetBool(v)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, err := strconv.ParseUint(raw, 10, p.bits)
		if err != nil {
			return err
		}
		field.SetUint(v)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := strconv.ParseInt(raw, 10, p.bits)
		if err != nil {
			return err
		}
		field.SetInt(v)
	default:
		return errors.New("unsupported kind")
	}
	return nil
}
