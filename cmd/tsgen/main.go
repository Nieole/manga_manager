// 业务说明：本文件是前后端契约类型的生成器（M47）。它以 Go 侧的响应结构体为单一事实源，
// 反射生成对应的 TypeScript 接口到 web/src/api/generated.ts，消除此前前端手写类型与后端各自漂移的问题。
// sql.Null* / time.Time 等按约定映射；sql.Null* 复用 web/src/api/contracts.ts 的单一定义（生成文件 import 之）。
// 维护时：需要新增受管契约类型时，把它加入 targets 列表即可；CI 会 `go run ./cmd/tsgen` 后 git diff 校验不漂移。
package main

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"manga-manager/internal/api"
)

// targets 是纳入生成的契约结构体（顺序即输出顺序）。被引用的具名结构体也必须在此列出。
var targets = []reflect.Type{
	reflect.TypeOf(api.TaskLimits{}),
	reflect.TypeOf(api.TaskStatus{}),
}

// nullTypes 记录 sql.Null* 到 contracts.ts 导出名的映射；用到哪些就 import 哪些。
var nullTypeNames = map[string]string{
	"database/sql.NullString":  "NullString",
	"database/sql.NullInt64":   "NullInt64",
	"database/sql.NullFloat64": "NullFloat64",
	"database/sql.NullTime":    "NullTime",
}

func main() {
	knownStructs := map[string]bool{}
	for _, t := range targets {
		knownStructs[t.Name()] = true
	}

	usedNulls := map[string]bool{}
	var body strings.Builder
	for _, t := range targets {
		emitInterface(t, knownStructs, usedNulls, &body)
	}

	var out strings.Builder
	out.WriteString("/**\n")
	out.WriteString(" * 业务说明：本文件由 cmd/tsgen 自动生成（M47），请勿手工编辑。\n")
	out.WriteString(" * 它以 Go 后端的响应结构体为单一事实源生成前端契约类型，防止手写类型与后端漂移。\n")
	out.WriteString(" * 重新生成：`go run ./cmd/tsgen`；CI 会校验其与源一致。\n")
	out.WriteString(" */\n\n")
	if len(usedNulls) > 0 {
		names := make([]string, 0, len(usedNulls))
		for n := range usedNulls {
			names = append(names, n)
		}
		sort.Strings(names)
		out.WriteString(fmt.Sprintf("import type { %s } from './contracts';\n\n", strings.Join(names, ", ")))
	}
	out.WriteString(body.String())

	const path = "web/src/api/generated.ts"
	if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
		os.Exit(1)
	}
	fmt.Printf("generated %s (%d types)\n", path, len(targets))
}

func emitInterface(t reflect.Type, known map[string]bool, usedNulls map[string]bool, b *strings.Builder) {
	fmt.Fprintf(b, "export interface %s {\n", t.Name())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		parts := strings.Split(tag, ",")
		name := parts[0]
		if name == "" {
			name = f.Name
		}
		omitempty := false
		for _, p := range parts[1:] {
			if p == "omitempty" {
				omitempty = true
			}
		}
		optional := omitempty || f.Type.Kind() == reflect.Ptr
		tsT := tsType(f.Type, known, usedNulls)
		opt := ""
		if optional {
			opt = "?"
		}
		fmt.Fprintf(b, "  %s%s: %s;\n", name, opt, tsT)
	}
	b.WriteString("}\n\n")
}

func tsType(t reflect.Type, known map[string]bool, usedNulls map[string]bool) string {
	switch t.Kind() {
	case reflect.Ptr:
		return tsType(t.Elem(), known, usedNulls)
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		elem := tsType(t.Elem(), known, usedNulls)
		if strings.ContainsAny(elem, " |") {
			return "(" + elem + ")[]"
		}
		return elem + "[]"
	case reflect.Map:
		return fmt.Sprintf("Record<%s, %s>", tsType(t.Key(), known, usedNulls), tsType(t.Elem(), known, usedNulls))
	case reflect.Struct:
		full := t.PkgPath() + "." + t.Name()
		if full == "time.Time" {
			return "string"
		}
		if nn, ok := nullTypeNames[full]; ok {
			usedNulls[nn] = true
			return nn
		}
		if known[t.Name()] {
			return t.Name()
		}
		// 未纳入生成的具名结构体：显式失败，避免静默产出 unknown。
		fmt.Fprintf(os.Stderr, "unhandled struct type %q (add it to targets)\n", full)
		os.Exit(1)
		return ""
	default:
		fmt.Fprintf(os.Stderr, "unhandled kind %s\n", t.Kind())
		os.Exit(1)
		return ""
	}
}
