// 本文件守提案子域的边界：internal/api 不得直接查提案表，读写一律经 internal/proposal。
// 破了这条，api 侧的捷径会绕开入队去重、按当前锁定集过滤，以及并发抢先的那道守卫。

package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// proposalTables 是提案子域独占的两张表。碰它们的查询必须由 internal/proposal 发起。
var proposalTables = []string{"metadata_reviews", "metadata_review_fields"}

// proposalTableQuerySymbols 从 sqlc 生成物推出「碰提案表的查询」这批方法名。
//
// 不写死清单：清单会腐坏——新加一条打提案表的查询，守卫却还按旧名字扫，看着绿其实没在守。
// 改从 SQL 自己认：生成的每个查询常量以 `-- name: 方法名 :kind` 开头，常量体就是那条 SQL。
func proposalTableQuerySymbols(t *testing.T) map[string]bool {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join("..", "database", "query.sql.go"), nil, 0)
	if err != nil {
		t.Fatalf("解析 sqlc 生成物失败: %v", err)
	}

	symbols := make(map[string]bool)
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			continue
		}
		for _, spec := range genDecl.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Values) != 1 {
				continue
			}
			lit, ok := value.Values[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			sql, err := strconv.Unquote(lit.Value)
			if err != nil {
				continue
			}
			if name := sqlcQueryName(sql); name != "" && mentionsProposalTable(sql) {
				symbols[name] = true
			}
		}
	}

	if len(symbols) == 0 {
		t.Fatal("没从生成物里认出任何打提案表的查询 —— 推导坏了，守卫会一直绿着什么也不守")
	}
	return symbols
}

// sqlcQueryName 取 `-- name: 方法名 :kind` 头里的方法名；不是查询常量则返回空串。
func sqlcQueryName(sql string) string {
	header, _, _ := strings.Cut(strings.TrimSpace(sql), "\n")
	rest, ok := strings.CutPrefix(header, "-- name:")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(strings.TrimSpace(rest), " ")
	return name
}

func mentionsProposalTable(sql string) bool {
	for _, table := range proposalTables {
		if strings.Contains(sql, table) {
			return true
		}
	}
	return false
}

// TestAPIDoesNotQueryProposalTables 扫 internal/api 的全部源码，子目录与用例文件一并扫。
//
// 这条守卫有真实对象：编译器在这里拦不住。api 仍握着全量的 database.Store——收窄经评估不做，
// 因为 ExecTx 交出的本就是具体的 *database.Queries，任何窄接口都拦不住调用方访问全部查询。
// 于是「顺手 c.store.GetMetadataReview 一下」永远编译得过。它守的是一条纪律，而纪律没有编译期
// 形态，因此不适用「结构性成立后守卫失去对象就该删」的先例。
//
// 不开白名单：第一条例外会把它变成一份需要维护的清单，而清单一旦有第一行就会有第二行。
// 用例也不例外——一个直接读提案行的断言与一次绕过模块的生产调用，在这条边界上是同一回事。
//
// 符号名从 sqlc 生成物反推，覆盖的因此正好是生成的那批查询。手写 SQL 与手写的 Store 方法
// 落在覆盖之外——今天两者都不存在（Store 不暴露裸执行），真要新增得连这条注释一起改。
func TestAPIDoesNotQueryProposalTables(t *testing.T) {
	forbidden := proposalTableQuerySymbols(t)

	scanned := 0
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		scanned++

		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || !forbidden[selector.Sel.Name] {
				return true
			}
			t.Errorf("%s:%d 直接调用了 %s —— 提案表只归 internal/proposal，"+
				"api 经这条捷径改状态会绕开去重、锁定过滤与并发抢先的守卫",
				path, fset.Position(selector.Sel.Pos()).Line, selector.Sel.Name)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("扫 internal/api 源码失败: %v", err)
	}

	if scanned == 0 {
		t.Fatal("一个源码文件都没扫到 —— 守卫的工作目录不对，它什么也没在守")
	}
}
