// 表结构与它的迁移。DDL 归本包而不是 internal/database/schema.sql：这几张表没有 sqlc 查询，
// 放进那份 schema 只会多出一批与领域类型重名的生成模型。

package taskstore

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"manga-manager/internal/task"
)

// 索引名。两条部分唯一索引是**准入**的落点，改名等于改判据，因此写成常量而不是散在 DDL 里。
const (
	indexRunsOneActivePerTask = "idx_runs_one_active_per_task"
	indexRunsOneQueuedPerTask = "idx_runs_one_queued_per_task"
)

// 表名。改这几个常量**不得**只改常量：`CREATE TABLE IF NOT EXISTS` 对已存在的表是无操作，
// 于是存量库旁边会另建一张空表，而外键仍指着原来那张。改名要另配一条 ALTER TABLE，
// legacyIdentityTable 那一段是先例。
const (
	tableTasks      = "tasks"
	tableRuns       = "runs"
	tableRunEvents  = "run_events"
	tableRunSamples = "run_samples"
	tableRunMetrics = "run_metrics"
	tableRunLimits  = "run_limits"
	tableRunArgs    = "run_args"
	tableRunLabels  = "run_labels"
)

// ErrForeignKeysDisabled 是连接没开 foreign_keys 时的哨兵错误。
//
// 拦在迁移而不是留到运行期：外键关掉时 CREATE TABLE 照建、DELETE 照跑，只是级联不发生，
// 于是事件与采样在运行被裁掉之后原地变成孤儿行，一年之后才以「库怎么这么大」的形式暴露。
var ErrForeignKeysDisabled = errors.New("taskstore requires PRAGMA foreign_keys=ON")

// createStatements 是建表语句。全部 IF NOT EXISTS，可重放。
//
// 时间列一律存 epoch 毫秒而不是文本：本仓已经吃过「两个写入方写出两种文本格式、而 SQLite 比的是
// 文本」的亏，保留裁剪的 DELETE 与聚合查询都要拿时间列做比较，整数没有这种歧义。
// 排序主键仍是 sequence，时间列不承担定序。
var createStatements = []string{
	`CREATE TABLE IF NOT EXISTS ` + tableTasks + ` (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		type TEXT NOT NULL,
		scope TEXT NOT NULL,
		scope_id INTEGER NOT NULL DEFAULT 0,
		variant TEXT NOT NULL DEFAULT '',
		disabled INTEGER NOT NULL DEFAULT 0,
		last_success_at INTEGER,
		fail_streak INTEGER NOT NULL DEFAULT 0,
		backoff_until INTEGER,
		UNIQUE(type, scope, scope_id, variant)
	)`,
	// scope_id 是 NOT NULL DEFAULT 0 而不是可空：唯一约束要比较这一列，而 SQL 里 NULL 不等于
	// NULL——留空的话同一个系统级身份会被建出任意多条。0 就是「系统级，没有作用域对象」。
	//
	// started_at 可空：**排队中**的运行还没开跑，写入队时刻会让排了一小时队的运行被算成跑了一小时。
	`CREATE TABLE IF NOT EXISTS ` + tableRuns + ` (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id INTEGER NOT NULL REFERENCES ` + tableTasks + `(id) ON DELETE CASCADE,
		scope_name TEXT NOT NULL DEFAULT '',
		trigger TEXT NOT NULL,
		nth_run INTEGER NOT NULL DEFAULT 1,
		status TEXT NOT NULL,
		phase TEXT NOT NULL DEFAULT '',
		current_item TEXT NOT NULL DEFAULT '',
		current INTEGER NOT NULL DEFAULT 0,
		total INTEGER NOT NULL DEFAULT 0,
		paused_at INTEGER,
		pause_reason TEXT NOT NULL DEFAULT '',
		control_paused_ms INTEGER NOT NULL DEFAULT 0,
		coalesced_count INTEGER NOT NULL DEFAULT 0,
		message_code TEXT NOT NULL DEFAULT '',
		message_params TEXT NOT NULL DEFAULT '',
		error TEXT NOT NULL DEFAULT '',
		started_at INTEGER,
		updated_at INTEGER,
		finished_at INTEGER,
		sequence INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS ` + tableRunEvents + ` (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		run_id INTEGER NOT NULL REFERENCES ` + tableRuns + `(id) ON DELETE CASCADE,
		at INTEGER NOT NULL,
		kind TEXT NOT NULL,
		payload TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS ` + tableRunSamples + ` (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		run_id INTEGER NOT NULL REFERENCES ` + tableRuns + `(id) ON DELETE CASCADE,
		at INTEGER NOT NULL,
		current INTEGER NOT NULL DEFAULT 0,
		throughput_per_minute REAL NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS ` + tableRunMetrics + ` (
		run_id INTEGER NOT NULL REFERENCES ` + tableRuns + `(id) ON DELETE CASCADE,
		key TEXT NOT NULL,
		value INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (run_id, key)
	)`,
	`CREATE TABLE IF NOT EXISTS ` + tableRunLimits + ` (
		run_id INTEGER PRIMARY KEY REFERENCES ` + tableRuns + `(id) ON DELETE CASCADE,
		scan_profile TEXT NOT NULL DEFAULT '',
		scanner_workers_configured INTEGER NOT NULL DEFAULT 0,
		scanner_workers_effective INTEGER NOT NULL DEFAULT 0,
		storage_profile TEXT NOT NULL DEFAULT '',
		volume_key TEXT NOT NULL DEFAULT '',
		scan_concurrency INTEGER NOT NULL DEFAULT 0,
		archive_open_concurrency INTEGER NOT NULL DEFAULT 0,
		cover_concurrency INTEGER NOT NULL DEFAULT 0,
		hash_concurrency INTEGER NOT NULL DEFAULT 0,
		pause_background_when_reading INTEGER NOT NULL DEFAULT 0,
		idle_only_heavy_tasks INTEGER NOT NULL DEFAULT 0,
		disable_same_disk_page_cache INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS ` + tableRunArgs + ` (
		run_id INTEGER NOT NULL REFERENCES ` + tableRuns + `(id) ON DELETE CASCADE,
		key TEXT NOT NULL,
		value TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (run_id, key)
	)`,
	// 展示标签自成一张表而不是并进 run_args：入参与标签是两类语义，同居一个命名空间正是
	// 旧 params 堆的成因，而读回时按前缀分拣的那把刀迟早会切错。
	`CREATE TABLE IF NOT EXISTS ` + tableRunLabels + ` (
		run_id INTEGER NOT NULL REFERENCES ` + tableRuns + `(id) ON DELETE CASCADE,
		key TEXT NOT NULL,
		value TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (run_id, key)
	)`,
}

// admissionStatements 是**准入**那两条部分唯一索引，每次迁移先删后建。
//
// 谓词由 task.ActiveStatuses 与 task.StatusQueued 拼出而不是手抄，而 `IF NOT EXISTS` 会让
// 存量库停在建库那天的谓词上：领域将来多一个活动态时，那些库照旧只索引老的三种，
// 第二条活动运行被静默放行——正是**准入由数据库保证**要防的那一件事。
// 重建几乎不要钱：部分索引只装谓词命中的行，而活着的运行任何时刻都只有个位数。
var admissionStatements = []string{
	`DROP INDEX IF EXISTS ` + indexRunsOneActivePerTask,
	`CREATE UNIQUE INDEX ` + indexRunsOneActivePerTask +
		` ON ` + tableRuns + `(task_id) WHERE status IN (` + quotedStatuses(task.ActiveStatuses()) + `)`,
	`DROP INDEX IF EXISTS ` + indexRunsOneQueuedPerTask,
	`CREATE UNIQUE INDEX ` + indexRunsOneQueuedPerTask +
		` ON ` + tableRuns + `(task_id) WHERE status = '` + string(task.StatusQueued) + `'`,
}

// addedColumns 是建表语句写下之后才追加的列。
//
// `CREATE TABLE IF NOT EXISTS` 对已存在的表是无操作，因此**改建表语句对存量库不生效**——
// 加列必须另走一条 `ALTER TABLE`（`internal/database` 的 ensureColumn 是先例）。这几张表还没有
// 随版本发布过，但开发机上早已按上一版建起，少了这一条它们会停在缺列的形状上。
var addedColumns = []struct{ table, column, definition string }{
	{tableRuns, "scope_name", `TEXT NOT NULL DEFAULT ''`},
	{tableRuns, "pause_reason", `TEXT NOT NULL DEFAULT ''`},
}

// renamedColumns 是建表语句写下之后改过名的列。
//
// 与 addedColumns 同理：`CREATE TABLE IF NOT EXISTS` 对已存在的表是无操作，改建表语句里的列名
// 对存量库不生效——而写入面已经按新名字插入了，少了这一条，开发机上那些库每落一个点都会撞上
// 「没有这一列」。改名而不是「加新列 + 留着旧列」：旧名下那些点是同一个数，搬过去就是全部。
var renamedColumns = []struct{ table, from, to string }{
	// 采样上这一格量的是「自上一个点以来那一段」的吞吐，与运行卡片上那个平均速率不是同一个数。
	{tableRunSamples, "rate_per_minute", "throughput_per_minute"},
}

// droppedColumns 是建表语句写下之后又撤掉的列，以及那一列上要一并丢掉的索引。
//
// 与 addedColumns 同理、方向相反：`CREATE TABLE IF NOT EXISTS` 对已存在的表是无操作，
// **从建表语句里删掉一列对存量库不生效**——撤列必须另走一条 `ALTER TABLE ... DROP COLUMN`，
// 少了这一条，开发机与用户库上那一列会一直留着，而写入面早已不再写它。
// 索引要随列一起给出：SQLite 不肯掉一个还被索引装着的列，而丢在别处会与掉列分家。
var droppedColumns = []struct {
	table, column string
	indexes       []string
}{
	// **任务键**退出寻址（ADR 0007）：控制端点一律按**运行**与**任务**寻址，投递出去的运行快照
	// 上也不再带它，关键词搜索改判在作用域显示名上——这一列因此一个读者都没有了。
	// 键本身没退场：它留在每一行日志上，唯一的来源是启动点写下的 task.RunSpec.Key。
	{tableRuns, "task_key", []string{"idx_runs_task_key"}},
}

// indexStatements 是取数用的索引，谓词不参与准入，因此建过就不必再动。
var indexStatements = []string{
	`CREATE INDEX IF NOT EXISTS idx_runs_sequence ON ` + tableRuns + `(sequence)`,
	`CREATE INDEX IF NOT EXISTS idx_runs_task_sequence ON ` + tableRuns + `(task_id, sequence)`,
	`CREATE INDEX IF NOT EXISTS idx_runs_status_sequence ON ` + tableRuns + `(status, sequence)`,
	`CREATE INDEX IF NOT EXISTS idx_run_events_run ON ` + tableRunEvents + `(run_id, id)`,
	`CREATE INDEX IF NOT EXISTS idx_run_events_run_kind ON ` + tableRunEvents + `(run_id, kind, id)`,
	`CREATE INDEX IF NOT EXISTS idx_run_samples_run ON ` + tableRunSamples + `(run_id, id)`,
	`CREATE INDEX IF NOT EXISTS idx_run_samples_at ON ` + tableRunSamples + `(at)`,
	// 身份表的唯一约束以 type 打头，按作用域找身份用不上它；LastRunIDsForScopes 正是这么找的，
	// 而它服务的是一条一次问上千个作用域的路径。
	`CREATE INDEX IF NOT EXISTS idx_tasks_scope ON ` + tableTasks + `(scope, scope_id)`,
}

// Migrate 建起任务与运行的表与索引。语句幂等，每次启动重放即可。
//
// `CREATE TABLE IF NOT EXISTS` 对已存在的表是无操作，因此将来给这几张表**加列**、**改列名**
// 或**撤列**都要另走一条 `ALTER TABLE`（见 addedColumns、renamedColumns 与 droppedColumns），
// 改这里的建表语句对存量库不生效。
//
// 前置条件：db 的连接必须开着 foreign_keys，否则返回 ErrForeignKeysDisabled。
func Migrate(db *sql.DB) error {
	var foreignKeys int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		return fmt.Errorf("taskstore: 读 foreign_keys 失败: %w", err)
	}
	if foreignKeys == 0 {
		return ErrForeignKeysDisabled
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// 改名排在建表之前：反过来的话 `CREATE TABLE IF NOT EXISTS tasks` 会先建出一张空表，
	// 改名随即撞名失败，而那张空表已经把身份行挡在外面了。
	if err := renameLegacyIdentityTable(tx); err != nil {
		return err
	}
	for _, stmt := range createStatements {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("taskstore: 执行迁移语句失败: %w", err)
		}
	}
	// 改名与补列都排在建表之后、建索引之前：索引可能就建在这几列上。
	// 改名先走，好让紧随其后的补列看见的是列的**当前**名字。
	for _, renamed := range renamedColumns {
		if err := ensureRenamedColumn(tx, renamed.table, renamed.from, renamed.to); err != nil {
			return err
		}
	}
	for _, added := range addedColumns {
		if err := ensureColumn(tx, added.table, added.column, added.definition); err != nil {
			return err
		}
	}
	// 撤列排在补列之后、建索引之前：撤掉的那一列上可能还挂着索引（它自己先丢），
	// 而排在建索引之后的话，刚建起的索引会在同一次迁移里被再丢一遍。
	for _, dropped := range droppedColumns {
		if err := dropColumnIfPresent(tx, dropped.table, dropped.column, dropped.indexes); err != nil {
			return err
		}
	}
	for _, stmt := range append(append([]string{}, admissionStatements...), indexStatements...) {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("taskstore: 执行迁移语句失败: %w", err)
		}
	}
	return tx.Commit()
}

// legacyIdentityTable 是身份表在存量库里可能还用着的名字。见到它就得改名过来：
// 库里的表名与规格的表形状、ADR 0004 和领域类型 task.Task 得是同一个字。
const legacyIdentityTable = "task_identities"

// legacyIdentityScopeIndex 是那个名字下的作用域索引。`ALTER TABLE RENAME` 不动索引名，
// 留着它就会与按新名建起的那条重复，因此改名之后要显式丢弃。
const legacyIdentityScopeIndex = "idx_task_identities_scope"

// renameLegacyIdentityTable 把身份表从 legacyIdentityTable 改名成 tableTasks，没有那张表就什么都不做。
//
// 走 RENAME 而不是「建一张空的 tasks 再把行搬过去」：`ALTER TABLE ... RENAME TO` 会一并改写
// 别的表 REFERENCES 子句里的表名（前提是没开 legacy_alter_table），runs 那条外键因此自己跟过来。
// 目标名已经被占时报错而不是静默跳过——那意味着调用方还没丢掉旧表，此刻改名会让两份身份并存。
func renameLegacyIdentityTable(tx *sql.Tx) error {
	present, err := tableExists(tx, legacyIdentityTable)
	if err != nil || !present {
		return err
	}
	occupied, err := tableExists(tx, tableTasks)
	if err != nil {
		return err
	}
	if occupied {
		return fmt.Errorf("taskstore: %s 与 %s 同时存在，改名前须先丢弃旧表", legacyIdentityTable, tableTasks)
	}
	if _, err := tx.Exec(`ALTER TABLE ` + legacyIdentityTable + ` RENAME TO ` + tableTasks); err != nil {
		return fmt.Errorf("taskstore: 把 %s 改名为 %s 失败: %w", legacyIdentityTable, tableTasks, err)
	}
	// 索引不随表改名，留着它会与随后按新名建起的那条索引重复。
	if _, err := tx.Exec(`DROP INDEX IF EXISTS ` + legacyIdentityScopeIndex); err != nil {
		return fmt.Errorf("taskstore: 丢弃索引 %s 失败: %w", legacyIdentityScopeIndex, err)
	}
	return nil
}

// tableExists 回答库里有没有这张表。
func tableExists(tx *sql.Tx, table string) (bool, error) {
	var name string
	err := tx.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("taskstore: 查表 %s 是否存在失败: %w", table, err)
	}
	return true, nil
}

// ensureColumn 给已存在的表补一列，已经有了就什么都不做。
//
// 先查后加而不是靠 `ALTER TABLE` 报错再吞：吞错误会把「列名拼错」这类真问题一起吞掉。
func ensureColumn(tx *sql.Tx, table, column, definition string) error {
	present, err := columnExists(tx, table, column)
	if err != nil || present {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition); err != nil {
		return fmt.Errorf("taskstore: 给 %s 补列 %s 失败: %w", table, column, err)
	}
	return nil
}

// ensureRenamedColumn 把一列改名，目标名已经在（含整张表刚由建表语句按新名建起）就什么都不做。
//
// 走 `RENAME COLUMN` 而不是「加一列再搬数」：改名是原地的，那一列上的值、类型与约束原样留着，
// 而搬数要多一句 UPDATE，且中途失败会留下两列各半份的采样。
// 先查后改而不是靠报错再吞：吞错误会把「表名拼错」这类真问题一起吞掉。
func ensureRenamedColumn(tx *sql.Tx, table, from, to string) error {
	present, err := columnExists(tx, table, to)
	if err != nil || present {
		return err
	}
	renamable, err := columnExists(tx, table, from)
	if err != nil || !renamable {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE ` + table + ` RENAME COLUMN ` + from + ` TO ` + to); err != nil {
		return fmt.Errorf("taskstore: 把 %s 的列 %s 改名为 %s 失败: %w", table, from, to, err)
	}
	return nil
}

// dropColumnIfPresent 撤掉一列，已经不在了就什么都不做；那一列上的索引先丢。
//
// 索引必须先走：SQLite 拒绝掉一个还被索引装着的列，而报出来的是一句「error in index」，
// 与「列名拼错」长得一样。
// 走 `DROP COLUMN` 而不是「留着列、只是不写它」：留着的话每条新运行在那一格上都是空串，
// 读到的人无从分辨那是「没人写」还是「本来就空」，而它还占着一条谁都不再维护的索引。
// 先查后掉而不是靠报错再吞：吞错误会把「表名拼错」这类真问题一起吞掉。
func dropColumnIfPresent(tx *sql.Tx, table, column string, indexes []string) error {
	present, err := columnExists(tx, table, column)
	if err != nil || !present {
		return err
	}
	for _, index := range indexes {
		if _, err := tx.Exec(`DROP INDEX IF EXISTS ` + index); err != nil {
			return fmt.Errorf("taskstore: 丢弃索引 %s 失败: %w", index, err)
		}
	}
	if _, err := tx.Exec(`ALTER TABLE ` + table + ` DROP COLUMN ` + column); err != nil {
		return fmt.Errorf("taskstore: 撤掉 %s 的列 %s 失败: %w", table, column, err)
	}
	return nil
}

// columnExists 回答这张表上有没有这一列；表本身不存在时一律回假。
func columnExists(tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.Query(`SELECT 1 FROM pragma_table_info(?) WHERE name = ?`, table, column)
	if err != nil {
		return false, fmt.Errorf("taskstore: 读 %s 的列失败: %w", table, err)
	}
	present := rows.Next()
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return false, fmt.Errorf("taskstore: 读 %s 的列失败: %w", table, err)
	}
	if err := rows.Close(); err != nil {
		return false, fmt.Errorf("taskstore: 读 %s 的列失败: %w", table, err)
	}
	return present, nil
}

// quotedStatuses 把一组状态拼成 SQL 的 IN 列表。状态取值是本仓自己的封闭枚举，不含引号。
func quotedStatuses(statuses []task.RunStatus) string {
	quoted := make([]string, 0, len(statuses))
	for _, status := range statuses {
		quoted = append(quoted, "'"+string(status)+"'")
	}
	return strings.Join(quoted, ", ")
}
