// 本文件守卫「起了任务的启动点，必须走带 panic 兜底的那条后台入口」。
//
// 这条约束没有编译期表达——`runBackground` 与 `runBackgroundTask` 都在 Controller 上，挑错
// 一个不会报错，只会在任务体 panic 时暴露：该任务键的活动任务永远停在 running、无法被清理，
// 从此恒定返回 409，须重启进程才能再发起同类任务。用源码扫描而非运行时断言，因为要守的
// 正是「有没有挑对函数」本身，运行时用例覆盖不到没人写用例的启动点。

package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// taskStartPrefix 是任务引擎上「占住一个任务键」的那族方法的共同前缀。
const taskStartPrefix = "start"

func TestTaskLaunchersUsePanicGuardedBackground(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("枚举 api 包源码失败: %v", err)
	}

	fset := token.NewFileSet()
	var violations []string
	scanned := 0
	for _, path := range sources {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", path, err)
		}
		scanned++
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			startsTask := false
			var unguarded []token.Pos
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				field, method := calleeSelector(call)
				switch {
				case field == "taskEngine" && strings.HasPrefix(method, taskStartPrefix):
					startsTask = true
				case method == "runBackground":
					unguarded = append(unguarded, call.Pos())
				}
				return true
			})
			if !startsTask {
				continue
			}
			for _, pos := range unguarded {
				violations = append(violations, fset.Position(pos).String()+" («"+fn.Name.Name+"»)")
			}
		}
	}

	// 扫不到文件时上面的循环会静默通过，那是最坏的失败形态：一条永远绿的守卫。
	if scanned == 0 {
		t.Fatal("没有扫到任何非测试源码，这条守卫等于没跑")
	}

	if len(violations) > 0 {
		t.Fatalf("以下启动点占住了任务键却走了无兜底的 runBackground，任务体 panic 后该任务键会恒定返回 409：\n  %s\n改用 runBackgroundTask（或迁到引擎的启动入口）。",
			strings.Join(violations, "\n  "))
	}
}

// calleeSelector 拆出调用的「接收者字段名」与「方法名」：
// `c.taskEngine.startTaskMsg(...)` → ("taskEngine", "startTaskMsg")；`c.runBackground(...)` → ("", "runBackground")。
func calleeSelector(call *ast.CallExpr) (field, method string) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", ""
	}
	if inner, ok := sel.X.(*ast.SelectorExpr); ok {
		field = inner.Sel.Name
	}
	return field, sel.Sel.Name
}
