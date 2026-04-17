import { Copy, KeyRound, RefreshCw, RotateCcw, Save, TabletSmartphone, Trash2, UserPlus } from 'lucide-react';
import { useSettings } from './SettingsContext';
import { FieldErrors, SettingsPageIntro, inputClassName, sectionClassName } from './shared';

export function SettingsKOReaderPage() {
  const {
    koreaderStatus,
    koreaderForm,
    setKOReaderForm,
    koreaderFieldErrors,
    saveKOReader,
    savingKOReader,
    needsMatchingMaintenance,
    applyingMatching,
    handleApplyMatchingChanges,
    handleAction,
    koreaderAccountForm,
    setKOReaderAccountForm,
    handleCreateKOReaderAccount,
    creatingAccount,
    koreaderAccounts,
    accountActionId,
    handleCopySyncKey,
    handleRotateKOReaderAccount,
    handleToggleKOReaderAccount,
    handleDeleteKOReaderAccount,
    unmatchedItems,
    fetchKOReaderUnmatched,
    formatKOReaderLatestSync,
    formatKOReaderIndexLabel,
  } = useSettings();

  if (!koreaderForm) return null;

  return (
    <div className="space-y-6">
      <SettingsPageIntro title="KOReader" description="单独管理 KOReader 服务配置、匹配规则、账号列表和未匹配记录。所有重建与重关联动作仍为即时执行任务。" />

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-sky-400">
          <TabletSmartphone className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">服务配置</h3>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div className="rounded-xl border border-gray-800 bg-gray-900/50 p-4">
            <label className="flex items-center justify-between gap-3">
              <div>
                <p className="text-sm font-medium text-white">启用 KOReader 同步服务</p>
                <p className="mt-1 text-xs text-gray-500">启用后，KOReader 可以将本程序作为自定义 progress sync server。</p>
              </div>
              <input
                type="checkbox"
                checked={koreaderForm.enabled}
                onChange={(e) => setKOReaderForm({ ...koreaderForm, enabled: e.target.checked })}
                className="h-5 w-5 rounded border-gray-700 bg-gray-900 text-komgaPrimary"
              />
            </label>
          </div>

          <div className="rounded-xl border border-gray-800 bg-gray-900/50 p-4">
            <p className="text-sm font-medium text-white">服务状态</p>
            <p className="mt-1 text-xs text-gray-500">已匹配 {koreaderStatus?.stats.matched_progress_count ?? 0} 条，同步待重关联 {koreaderStatus?.stats.unmatched_progress_count ?? 0} 条。</p>
            <p className="mt-2 text-xs text-gray-500">
              {formatKOReaderIndexLabel(koreaderForm.match_mode, koreaderForm.path_ignore_extension)} 进度 {koreaderStatus?.stats.hashed_books ?? 0} / {koreaderStatus?.stats.total_books ?? 0}
            </p>
            <p className="mt-2 text-xs text-gray-500">KOReader 账号 {koreaderStatus?.enabled_account_count ?? 0} / {koreaderStatus?.account_count ?? 0} 已启用。</p>
            <p className="mt-2 text-xs text-gray-500">最近同步 {formatKOReaderLatestSync(koreaderStatus?.stats.latest_sync_at)}</p>
            {koreaderStatus?.latest_error && <p className="mt-2 text-xs text-amber-300">最近错误 {koreaderStatus.latest_error}</p>}
          </div>

          <div>
            <label className="mb-1 block text-sm text-gray-400">同步路径</label>
            <input type="text" value={koreaderForm.base_path} onChange={(e) => setKOReaderForm({ ...koreaderForm, base_path: e.target.value })} className={inputClassName} />
            <p className="mt-1 text-xs text-gray-500">当前启动实例监听在 `{koreaderStatus?.base_path || '/koreader'}`。修改路径后建议重启服务。</p>
            <FieldErrors messages={koreaderFieldErrors('koreader.base_path')} />
          </div>

          <div>
            <label className="mb-1 block text-sm text-gray-400">允许首次注册</label>
            <select
              value={koreaderForm.allow_registration ? 'true' : 'false'}
              onChange={(e) => setKOReaderForm({ ...koreaderForm, allow_registration: e.target.value === 'true' })}
              className={inputClassName}
            >
              <option value="false">关闭</option>
              <option value="true">开启</option>
            </select>
            <p className="mt-1 text-xs text-gray-500">多账号 + 服务端生成 Sync Key 模式下，仍建议关闭设备侧自助注册。</p>
          </div>

          <div>
            <label className="mb-1 block text-sm text-gray-400">匹配模式</label>
            <select value={koreaderForm.match_mode} onChange={(e) => setKOReaderForm({ ...koreaderForm, match_mode: e.target.value })} className={inputClassName}>
              <option value="binary_hash">二进制哈希</option>
              <option value="file_path">文件路径</option>
            </select>
            <p className="mt-1 text-xs text-gray-500">`file_path` 模式只比较文件名及向上 {koreaderStatus?.path_match_depth ?? 2} 层路径。</p>
            <FieldErrors messages={koreaderFieldErrors('koreader.match_mode')} />
          </div>

          <div className="rounded-xl border border-gray-800 bg-gray-900/50 p-4">
            <label className="flex items-center justify-between gap-3">
              <div>
                <p className="text-sm font-medium text-white">路径匹配时忽略扩展名</p>
                <p className="mt-1 text-xs text-gray-500">仅在 `file_path` 模式下生效。</p>
              </div>
              <input
                type="checkbox"
                checked={koreaderForm.path_ignore_extension}
                disabled={koreaderForm.match_mode !== 'file_path'}
                onChange={(e) => setKOReaderForm({ ...koreaderForm, path_ignore_extension: e.target.checked })}
                className="h-5 w-5 rounded border-gray-700 bg-gray-900 text-komgaPrimary disabled:opacity-50"
              />
            </label>
          </div>
        </div>

        <div className="rounded-xl border border-sky-500/20 bg-sky-500/5 p-4 text-sm text-sky-100">
          <p className="font-medium">KOReader 配置方式</p>
          <p className="mt-1 text-sky-100/80">
            在 KOReader 中将 Custom sync server 设置为 `{window.location.origin}{koreaderStatus?.base_path || '/koreader'}`。用户名和下方显示的原始 Sync Key 直接填到设备即可。
          </p>
        </div>

        {needsMatchingMaintenance && (
          <div className="rounded-xl border border-amber-500/20 bg-amber-500/10 p-4 text-sm text-amber-100">
            <p className="font-medium">匹配规则已变更</p>
            <p className="mt-1 text-amber-100/80">建议立即应用变更，系统会顺序执行重建当前索引和重关联未匹配记录。</p>
            <button onClick={handleApplyMatchingChanges} disabled={applyingMatching} className="mt-4 inline-flex items-center gap-2 rounded-lg border border-amber-500/20 bg-black/20 px-4 py-2 text-sm text-amber-50 hover:bg-black/30 disabled:opacity-60">
              <RefreshCw className={`h-4 w-4 ${applyingMatching ? 'animate-spin' : ''}`} />
              {applyingMatching ? '提交中...' : '应用匹配规则变更'}
            </button>
          </div>
        )}

        <div className="grid gap-3 md:grid-cols-3">
          <button onClick={saveKOReader} disabled={savingKOReader} className="rounded-xl border border-sky-500/20 bg-sky-500/10 px-4 py-4 text-left text-sky-100 hover:bg-sky-500/15 disabled:opacity-60">
            <p className="inline-flex items-center gap-2 font-medium">
              <Save className={`h-4 w-4 ${savingKOReader ? 'animate-spin' : ''}`} />
              {savingKOReader ? '保存中...' : '保存同步配置'}
            </p>
            <p className="mt-1 text-xs text-sky-100/80">只保存服务级配置。账号和 Sync Key 通过下方账号列表管理。</p>
          </button>
          <button onClick={() => handleAction('/api/system/koreader/rebuild-hashes', 'KOReader 索引重建已启动')} className="rounded-xl border border-sky-500/20 bg-sky-500/10 px-4 py-4 text-left text-sky-100 hover:bg-sky-500/15">
            <p className="font-medium">重建 {formatKOReaderIndexLabel(koreaderForm.match_mode, koreaderForm.path_ignore_extension)}</p>
            <p className="mt-1 text-xs text-sky-100/80">按当前模式为现有书籍补全 KOReader 所需索引。</p>
          </button>
          <button onClick={() => handleAction('/api/system/koreader/reconcile', '未匹配同步记录重关联已启动')} className="rounded-xl border border-sky-500/20 bg-sky-500/10 px-4 py-4 text-left text-sky-100 hover:bg-sky-500/15">
            <p className="font-medium">重关联未匹配记录</p>
            <p className="mt-1 text-xs text-sky-100/80">重新尝试把历史同步记录映射回已入库书籍。</p>
          </button>
        </div>
      </section>

      <section className={sectionClassName}>
        <h3 className="text-lg font-semibold text-white">账号管理</h3>
        <div className="flex flex-col gap-3 md:flex-row md:items-end">
          <div className="flex-1">
            <label className="mb-1 block text-sm text-gray-400">新增 KOReader 账号</label>
            <input type="text" value={koreaderAccountForm.username} onChange={(e) => setKOReaderAccountForm({ username: e.target.value })} className={inputClassName} placeholder="输入唯一用户名" />
            <FieldErrors messages={koreaderFieldErrors('koreader.accounts.username')} />
          </div>
          <button onClick={handleCreateKOReaderAccount} disabled={creatingAccount || !koreaderAccountForm.username.trim()} className="inline-flex items-center justify-center gap-2 rounded-lg border border-sky-500/20 bg-sky-500/10 px-4 py-2.5 text-sm text-sky-100 hover:bg-sky-500/15 disabled:opacity-60">
            {creatingAccount ? <RefreshCw className="h-4 w-4 animate-spin" /> : <UserPlus className="h-4 w-4" />}
            {creatingAccount ? '创建中...' : '创建账号并生成 Sync Key'}
          </button>
        </div>

        <div className="space-y-3">
          {koreaderAccounts.length === 0 ? (
            <p className="text-sm text-gray-500">当前还没有 KOReader 账号。创建后系统会生成原始 Sync Key，直接填到设备即可。</p>
          ) : (
            koreaderAccounts.map((account) => (
              <div key={account.id} className="rounded-lg border border-gray-800 bg-black/20 p-4 space-y-3">
                <div className="flex flex-col gap-2 md:flex-row md:items-start md:justify-between">
                  <div>
                    <p className="text-sm font-medium text-white">{account.username}</p>
                    <p className="mt-1 text-xs text-gray-500">状态 {account.enabled ? '已启用' : '已停用'} · 最近使用 {account.last_used_at ? new Date(account.last_used_at).toLocaleString() : '暂无'}</p>
                    {account.latest_error && <p className="mt-1 text-xs text-amber-300">最近错误 {account.latest_error}</p>}
                  </div>
                  <div className="text-xs text-gray-500">创建于 {new Date(account.created_at).toLocaleString()}</div>
                </div>
                <div className="rounded-lg border border-gray-800 bg-gray-950 px-3 py-2">
                  <p className="text-[11px] uppercase tracking-wide text-gray-500">原始 Sync Key</p>
                  <p className="mt-1 break-all font-mono text-sm text-sky-100">{account.sync_key}</p>
                </div>
                <div className="flex flex-wrap gap-2">
                  <button onClick={() => handleCopySyncKey(account)} className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-black/20 px-3 py-2 text-xs text-gray-200 hover:bg-black/30">
                    <Copy className="h-3.5 w-3.5" />
                    复制 Sync Key
                  </button>
                  <button onClick={() => handleRotateKOReaderAccount(account)} disabled={accountActionId === account.id} className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-black/20 px-3 py-2 text-xs text-gray-200 hover:bg-black/30 disabled:opacity-60">
                    {accountActionId === account.id ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <RotateCcw className="h-3.5 w-3.5" />}
                    轮换 Sync Key
                  </button>
                  <button onClick={() => handleToggleKOReaderAccount(account)} disabled={accountActionId === account.id} className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-black/20 px-3 py-2 text-xs text-gray-200 hover:bg-black/30 disabled:opacity-60">
                    <KeyRound className="h-3.5 w-3.5" />
                    {account.enabled ? '停用账号' : '启用账号'}
                  </button>
                  <button onClick={() => handleDeleteKOReaderAccount(account)} disabled={accountActionId === account.id} className="inline-flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs text-red-200 hover:bg-red-500/15 disabled:opacity-60">
                    <Trash2 className="h-3.5 w-3.5" />
                    删除账号
                  </button>
                </div>
              </div>
            ))
          )}
        </div>
      </section>

      <section className={sectionClassName}>
        <div className="flex items-center justify-between gap-3">
          <div>
            <h3 className="text-lg font-semibold text-white">未匹配同步记录</h3>
            <p className="mt-1 text-sm text-gray-400">这里展示最近未能映射回本地书籍的 KOReader 记录，方便排查路径规则或索引状态。</p>
          </div>
          <button onClick={fetchKOReaderUnmatched} className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-black/20 px-3 py-2 text-xs text-gray-200 hover:bg-black/30">
            <RefreshCw className="h-3.5 w-3.5" />
            刷新
          </button>
        </div>
        <div className="space-y-3">
          {unmatchedItems.length === 0 ? (
            <p className="text-sm text-gray-500">当前没有未匹配的 KOReader 同步记录。</p>
          ) : (
            unmatchedItems.map((item) => (
              <div key={item.id} className="rounded-lg border border-gray-800 bg-black/20 p-3">
                <div className="flex flex-col gap-2 md:flex-row md:items-start md:justify-between">
                  <div className="min-w-0">
                    <p className="text-sm font-medium text-white break-all">{item.document}</p>
                    <p className="mt-1 text-xs text-gray-500 break-all">当前匹配键：{item.normalized_key || '无法归一化'}</p>
                  </div>
                  <div className="text-xs text-gray-500">{Math.round(item.percentage * 100)}% · {new Date(item.updated_at).toLocaleString()}</div>
                </div>
                <p className="mt-2 text-xs text-gray-400">设备：{item.device || '未知设备'}{item.device_id ? ` (${item.device_id})` : ''}</p>
                <p className="mt-2 text-xs text-amber-200/90">{item.suggestion}</p>
              </div>
            ))
          )}
        </div>
      </section>
    </div>
  );
}
