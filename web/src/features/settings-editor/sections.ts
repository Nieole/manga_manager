import type { Config } from './types';

// connections 也是一个配置分区（协议开关落在 config.protocols 下），只是它没有 SaveBar。
// 开关即时保存，不先改草稿；仍使用同一套分区基线和版本规则。
export type ConfigSectionKey = 'library' | 'media' | 'ai' | 'connections';
export type EditorSectionKey = ConfigSectionKey | 'koreader';

// SECTION_FIELD_PATHS 是「哪些配置字段属于哪个分区」的**唯一**事实源。
//
// 脏标记与保存必须用同一份定义，否则两者会漂移：脏标记说这个分区没改，保存却把它写了出去。
// 路径用点号表示，只到需要区分的那一层——例如 scanner 下只有部分字段属于 library 分区，
// 其余属于 media 分区，所以这里必须写到叶子。
export const SECTION_FIELD_PATHS: Record<EditorSectionKey, string[]> = {
  library: [
    'server',
    'database',
    'library',
    'logging',
    'scanner.workers',
    'scanner.scan_profile',
    'scanner.archive_pool_size',
    // 后台运行槽位与上面那几个并发数是同一排问题（「一次并行多少」），因此同属这个分区。
    'tasks',
  ],
  media: [
    'cache',
    'scanner.thumbnail_format',
    'scanner.waifu2x_path',
    'scanner.realcugan_path',
    'scanner.max_ai_concurrency',
  ],
  ai: ['llm'],
  // protocols 整块属于连接分区：opds / mihon 两个子键没有被别的分区瓜分，无需写到叶子。
  connections: ['protocols'],
  koreader: ['koreader'],
};

function readPath(source: unknown, path: string): unknown {
  return path.split('.').reduce<unknown>((acc, key) => {
    if (acc && typeof acc === 'object') return (acc as Record<string, unknown>)[key];
    return undefined;
  }, source);
}

// writePath 在 target 上按路径写值，沿途只复制被改动的那一层，其余保持原引用。
function writePath(target: Record<string, unknown>, path: string, value: unknown): void {
  const keys = path.split('.');
  let node = target;
  for (let i = 0; i < keys.length - 1; i += 1) {
    const key = keys[i];
    const child = node[key];
    node[key] = child && typeof child === 'object' ? { ...(child as Record<string, unknown>) } : {};
    node = node[key] as Record<string, unknown>;
  }
  node[keys[keys.length - 1]] = value;
}

export function pickSectionSnapshot(config: Config, section: EditorSectionKey) {
  const snapshot: Record<string, unknown> = {};
  for (const path of SECTION_FIELD_PATHS[section]) {
    writePath(snapshot, path, readPath(config, path));
  }
  return snapshot;
}

// applySectionSnapshot 把 draft 里**只属于该分区**的字段叠到 base 上。
//
// 这是「保存本分区」真正的语义：saveConfig 不得把整份 config state 发出去，否则
// 用户在**其他分区**改了却没保存的草稿会被一并静默写进后端，界面上不会有任何提示。
export function applySectionSnapshot(base: Config, draft: Config, section: EditorSectionKey): Config {
  const merged = { ...(base as unknown as Record<string, unknown>) };
  for (const path of SECTION_FIELD_PATHS[section]) {
    writePath(merged, path, readPath(draft, path));
  }
  return merged as unknown as Config;
}

function acceptUnedited(current: unknown, sent: unknown, saved: unknown): unknown {
  if (JSON.stringify(current) === JSON.stringify(sent)) return saved;
  if (current && sent && saved && typeof current === 'object' && typeof sent === 'object' && typeof saved === 'object'
    && !Array.isArray(current) && !Array.isArray(sent) && !Array.isArray(saved)) {
    const result = { ...current } as Record<string, unknown>;
    for (const key of new Set([...Object.keys(sent), ...Object.keys(saved)])) {
      result[key] = acceptUnedited((current as Record<string, unknown>)[key], (sent as Record<string, unknown>)[key], (saved as Record<string, unknown>)[key]);
    }
    return result;
  }
  return current;
}

export function acceptSectionSave(current: Config, sent: Config, saved: Config, section: EditorSectionKey): Config {
  const next = { ...current } as unknown as Record<string, unknown>;
  for (const path of SECTION_FIELD_PATHS[section]) {
    writePath(next, path, acceptUnedited(readPath(current, path), readPath(sent, path), readPath(saved, path)));
  }
  return next as unknown as Config;
}
