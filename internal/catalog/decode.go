// TOML decoding reports independent type and unknown-field errors together.
package catalog

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

var (
	timeType   = reflect.TypeOf(time.Time{})
	arrayIndex = regexp.MustCompile(`\[[0-9]+\]`)
)

type reporter func(field, format string, args ...any)

// decode checks exact key spelling and every field's type. A separate typed
// decode checks MetaData.Undecoded; the tree walk also works when a type error
// would cause that decoder to stop before reaching other fields.
func decode(data []byte, dst any, report reporter) bool {
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		report("toml", "%v", err)
		return false
	}
	unknown := make(map[string]bool)
	populate(reflect.ValueOf(dst).Elem(), raw, "", report, unknown)

	check := reflect.New(reflect.TypeOf(dst).Elem()).Interface()
	if meta, err := toml.Decode(string(data), check); err == nil {
		for _, key := range meta.Undecoded() {
			field := key.String()
			covered := false
			for path := range unknown {
				if field == path || strings.HasPrefix(field, path+".") {
					covered = true
					break
				}
			}
			if !covered {
				report(field, "unknown field")
			}
		}
	}
	return true
}

func childPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func populate(dst reflect.Value, src any, path string, report reporter, unknown map[string]bool) {
	if dst.Kind() == reflect.Pointer {
		dst.Set(reflect.New(dst.Type().Elem()))
		populate(dst.Elem(), src, path, report, unknown)
		return
	}
	if dst.Type() == timeType {
		if v, ok := src.(time.Time); ok {
			dst.Set(reflect.ValueOf(v))
		} else {
			report(path, "must be a TOML local date")
		}
		return
	}
	switch dst.Kind() {
	case reflect.Struct:
		m, ok := src.(map[string]any)
		if !ok {
			report(path, "must be a table")
			return
		}
		fields := make(map[string]int)
		for i := 0; i < dst.NumField(); i++ {
			f := dst.Type().Field(i)
			tag := f.Tag.Get("toml")
			if f.PkgPath == "" && tag != "" && tag != "-" {
				fields[tag] = i
			}
		}
		for key, value := range m {
			field := childPath(path, key)
			if i, ok := fields[key]; ok {
				populate(dst.Field(i), value, field, report, unknown)
			} else {
				report(field, "unknown field")
				unknown[arrayIndex.ReplaceAllString(field, "")] = true
			}
		}
	case reflect.Slice:
		v := reflect.ValueOf(src)
		if !v.IsValid() || v.Kind() != reflect.Slice {
			report(path, "must be an array of tables")
			return
		}
		dst.Set(reflect.MakeSlice(dst.Type(), v.Len(), v.Len()))
		for i := 0; i < v.Len(); i++ {
			populate(dst.Index(i), v.Index(i).Interface(), fmt.Sprintf("%s[%d]", path, i), report, unknown)
		}
	case reflect.String:
		if v, ok := src.(string); ok {
			dst.SetString(v)
		} else {
			report(path, "must be a string")
		}
	case reflect.Bool:
		if v, ok := src.(bool); ok {
			dst.SetBool(v)
		} else {
			report(path, "must be a boolean")
		}
	default:
		panic("unsupported catalog schema type: " + dst.Type().String())
	}
}
