/**
 * 本文件是前后端契约的共享原语，集中定义 Go 后端 `database/sql` 的 `sql.Null*`
 * 类型在 JSON 序列化后的前端镜像（形如 { <T>, Valid }）。各页面 types.ts 须统一从
 * 这里再导出（re-export），不得各自重复声明，否则容易与后端字段各自漂移。
 * 维护时应保证这里的形状与 Go 侧 sql.Null* 的 JSON 编码严格一致。
 */

/** 镜像 Go `sql.NullString` 的 JSON 形状。 */
export interface NullString {
  String: string;
  Valid: boolean;
}

/** 镜像 Go `sql.NullInt64` 的 JSON 形状。 */
export interface NullInt64 {
  Int64: number;
  Valid: boolean;
}

/** 镜像 Go `sql.NullTime` 的 JSON 形状（Time 为 RFC3339 字符串）。 */
export interface NullTime {
  Time: string;
  Valid: boolean;
}

/** 镜像 Go `sql.NullFloat64` 的 JSON 形状。 */
export interface NullFloat64 {
  Float64: number;
  Valid: boolean;
}
