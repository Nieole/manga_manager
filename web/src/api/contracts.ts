/**
 * 业务说明：本文件是前后端契约的共享原语，集中定义 Go 后端 `database/sql` 的 `sql.Null*`
 * 类型在 JSON 序列化后的前端镜像（形如 { <T>, Valid }）。此前这些接口在多个页面的
 * types.ts 中各自重复声明，容易与后端字段各自漂移；收敛到此单一来源，各页面 types.ts
 * 统一从这里再导出（re-export），既消除重复，又不改动任何既有 import 路径。
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
