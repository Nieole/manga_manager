// JSON 响应里宿主机绝对路径的按角色净化，落在 jsonResponse 这一个出口上：
// 只有 authGate 认定为管理员的响应保留路径字段，其余（含拿不到用户的）一律置空。

package api

import (
	"net/http"
	"reflect"
	"strings"
	"sync"

	"manga-manager/internal/database"
)

// hostPathJSONNames 是判定为「宿主机文件系统路径」的字段名，按 json 标签匹配（无标签则按字段名小写）。
// 判定按名字而非按类型，是为了让**将来新增**的响应结构体默认落进净化：
// 忘了登记的新字段会被裁掉（功能上看得见），而不是被放行（安全上看不见）。
var hostPathJSONNames = map[string]struct{}{
	"path":          {},
	"library_path":  {},
	"book_path":     {},
	"external_path": {},
}

// hostPathExemptTypes 是名字命中却不该裁的类型：这两处对普通用户回显路径是**现行可见行为**
// （整理页的健康问题列表、读不到字节时的错误提示），改动需要产品裁决，不在净化范围内。
var hostPathExemptTypes = map[reflect.Type]struct{}{
	reflect.TypeOf(database.HealthIssue{}):   {},
	reflect.TypeOf(StorageFailureResponse{}): {},
}

// adminResponseWriter 是 authGate 给管理员请求套的标记层：jsonResponse 只认这一个类型，
// 因此任何绕过 authGate 或拿不到用户的响应都落在「非管理员」一侧（fail-closed）。
type adminResponseWriter struct {
	http.ResponseWriter
}

// Flush 透传给底层，否则 SSE 一类流式响应会被这层包装憋住。
func (w *adminResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap 让 http.ResponseController 能穿透本层拿到底层能力。
func (w *adminResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// responseKeepsHostPaths 报告这次响应是否保留路径字段。
func responseKeepsHostPaths(w http.ResponseWriter) bool {
	_, ok := w.(*adminResponseWriter)
	return ok
}

// redactHostPaths 返回 v 的净化副本：命中的字符串字段置空，其余原样。
//
// 绝不就地改写入参——响应值可能来自进程内缓存或 store 复用的行，就地置空会把管理员那一份
// 也一起抹掉，且是下一次请求才显形的错。没有命中时原值原样返回，不产生任何拷贝。
func redactHostPaths(v any) any {
	if v == nil {
		return nil
	}
	out, changed := redactValue(reflect.ValueOf(v))
	if !changed {
		return v
	}
	return out.Interface()
}

// redactValue 递归净化一个值，返回（新值, 是否有改动）。未改动时返回原值。
func redactValue(v reflect.Value) (reflect.Value, bool) {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return v, false
		}
		elem, changed := redactValue(v.Elem())
		if !changed {
			return v, false
		}
		nv := reflect.New(v.Type()).Elem()
		nv.Set(elem)
		return nv, true
	case reflect.Pointer:
		if v.IsNil() {
			return v, false
		}
		elem, changed := redactValue(v.Elem())
		if !changed {
			return v, false
		}
		np := reflect.New(v.Type().Elem())
		np.Elem().Set(elem)
		return np, true
	case reflect.Slice, reflect.Array:
		return redactList(v)
	case reflect.Map:
		return redactMap(v)
	case reflect.Struct:
		return redactStruct(v)
	default:
		return v, false
	}
}

func redactList(v reflect.Value) (reflect.Value, bool) {
	if v.Kind() == reflect.Slice && v.IsNil() {
		return v, false
	}
	if !typeMayHoldHostPath(v.Type().Elem()) {
		return v, false
	}
	n := v.Len()
	var out reflect.Value
	changed := false
	for i := 0; i < n; i++ {
		ev, ok := redactValue(v.Index(i))
		if !ok {
			continue
		}
		if !changed {
			if v.Kind() == reflect.Slice {
				out = reflect.MakeSlice(v.Type(), n, n)
			} else {
				out = reflect.New(v.Type()).Elem()
			}
			reflect.Copy(out, v)
			changed = true
		}
		out.Index(i).Set(ev)
	}
	if !changed {
		return v, false
	}
	return out, true
}

func redactMap(v reflect.Value) (reflect.Value, bool) {
	if v.IsNil() || !typeMayHoldHostPath(v.Type().Elem()) {
		return v, false
	}
	var out reflect.Value
	changed := false
	for iter := v.MapRange(); iter.Next(); {
		ev, ok := redactValue(iter.Value())
		if !ok {
			continue
		}
		if !changed {
			out = reflect.MakeMapWithSize(v.Type(), v.Len())
			for copyIter := v.MapRange(); copyIter.Next(); {
				out.SetMapIndex(copyIter.Key(), copyIter.Value())
			}
			changed = true
		}
		out.SetMapIndex(iter.Key(), ev)
	}
	if !changed {
		return v, false
	}
	return out, true
}

func redactStruct(v reflect.Value) (reflect.Value, bool) {
	plan := structRedactionPlan(v.Type())
	if plan == nil {
		return v, false
	}
	nv := reflect.New(v.Type()).Elem()
	nv.Set(v)
	changed := false
	for _, i := range plan.blank {
		if nv.Field(i).String() != "" {
			nv.Field(i).SetString("")
			changed = true
		}
	}
	for _, i := range plan.recurse {
		fv, ok := redactValue(v.Field(i))
		if ok {
			nv.Field(i).Set(fv)
			changed = true
		}
	}
	if !changed {
		return v, false
	}
	return nv, true
}

// redactionPlan 记录某个结构体类型上要置空的字段与要继续下钻的字段（均为字段下标）。
type redactionPlan struct {
	blank   []int
	recurse []int
}

var (
	structPlanCache sync.Map // reflect.Type -> *redactionPlan（nil 表示无事可做）
	mayHoldCache    sync.Map // reflect.Type -> bool
)

// structRedactionPlan 返回类型 t 的净化方案；无事可做时返回 nil。
func structRedactionPlan(t reflect.Type) *redactionPlan {
	if cached, ok := structPlanCache.Load(t); ok {
		plan, _ := cached.(*redactionPlan)
		return plan
	}
	var plan *redactionPlan
	if _, exempt := hostPathExemptTypes[t]; !exempt {
		var blank, recurse []int
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" { // 未导出字段不进 JSON，也不可写
				continue
			}
			if f.Type.Kind() == reflect.String && isHostPathFieldName(f) {
				blank = append(blank, i)
				continue
			}
			if typeMayHoldHostPath(f.Type) {
				recurse = append(recurse, i)
			}
		}
		if len(blank) > 0 || len(recurse) > 0 {
			plan = &redactionPlan{blank: blank, recurse: recurse}
		}
	}
	structPlanCache.Store(t, plan)
	return plan
}

// typeMayHoldHostPath 报告类型 t 有没有可能带上路径字段。interface{} 一律按「有可能」处理，
// 真实类型到运行时再看——响应常以 map[string]interface{} 组装，静态判不出来。
func typeMayHoldHostPath(t reflect.Type) bool {
	if cached, ok := mayHoldCache.Load(t); ok {
		return cached.(bool)
	}
	result := computeMayHoldHostPath(t, map[reflect.Type]bool{})
	mayHoldCache.Store(t, result)
	return result
}

func computeMayHoldHostPath(t reflect.Type, visiting map[reflect.Type]bool) bool {
	if visiting[t] {
		return false // 自引用类型：这一支已在上层判过，避免无限递归
	}
	switch t.Kind() {
	case reflect.Interface:
		return true
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		visiting[t] = true
		defer delete(visiting, t)
		return computeMayHoldHostPath(t.Elem(), visiting)
	case reflect.Struct:
		if _, exempt := hostPathExemptTypes[t]; exempt {
			return false
		}
		visiting[t] = true
		defer delete(visiting, t)
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue
			}
			if f.Type.Kind() == reflect.String && isHostPathFieldName(f) {
				return true
			}
			if computeMayHoldHostPath(f.Type, visiting) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// isHostPathFieldName 按 json 标签名判定字段是不是宿主机路径；无标签时退回字段名小写。
func isHostPathFieldName(f reflect.StructField) bool {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	if name == "-" {
		return false
	}
	if name == "" {
		name = strings.ToLower(f.Name)
	}
	_, ok := hostPathJSONNames[name]
	return ok
}
