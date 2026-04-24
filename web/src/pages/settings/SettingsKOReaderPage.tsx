import { Copy, KeyRound, RefreshCw, RotateCcw, Save, TabletSmartphone, Trash2, UserPlus } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { useSettings } from './SettingsContext';
import { FieldErrors, SettingsPageIntro, inputClassName, sectionClassName } from './shared';

export function SettingsKOReaderPage() {
  const { t, formatDateTime } = useI18n();
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
      <SettingsPageIntro title={t('app.koreader')} description={t('settings.koreader.description')} />

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-komgaSecondary">
          <TabletSmartphone className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">{t('settings.koreader.serviceTitle')}</h3>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div className="rounded-xl border border-gray-800 bg-gray-900/50 p-4">
            <label className="flex items-center justify-between gap-3">
              <div>
                <p className="text-sm font-medium text-white">{t('settings.koreader.enableTitle')}</p>
                <p className="mt-1 text-xs text-gray-500">{t('settings.koreader.enableDescription')}</p>
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
            <p className="text-sm font-medium text-white">{t('settings.koreader.statusTitle')}</p>
            <p className="mt-1 text-xs text-gray-500">{t('settings.koreader.statusSummary', { matched: koreaderStatus?.stats.matched_progress_count ?? 0, unmatched: koreaderStatus?.stats.unmatched_progress_count ?? 0 })}</p>
            <p className="mt-2 text-xs text-gray-500">
              {t('settings.koreader.indexProgress', {
                label: formatKOReaderIndexLabel(koreaderForm.match_mode, koreaderForm.path_ignore_extension),
                current: koreaderStatus?.stats.hashed_books ?? 0,
                total: koreaderStatus?.stats.total_books ?? 0,
              })}
            </p>
            <p className="mt-2 text-xs text-gray-500">{t('settings.koreader.accountsEnabled', { enabled: koreaderStatus?.enabled_account_count ?? 0, total: koreaderStatus?.account_count ?? 0 })}</p>
            <p className="mt-2 text-xs text-gray-500">{t('settings.koreader.latestSync', { value: formatKOReaderLatestSync(koreaderStatus?.stats.latest_sync_at) })}</p>
            {koreaderStatus?.latest_error && <p className="mt-2 text-xs text-red-500">{t('settings.koreader.latestError', { error: koreaderStatus.latest_error })}</p>}
          </div>

          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.koreader.basePath')}</label>
            <input type="text" value={koreaderForm.base_path} onChange={(e) => setKOReaderForm({ ...koreaderForm, base_path: e.target.value })} className={inputClassName} />
            <p className="mt-1 text-xs text-gray-500">{t('settings.koreader.basePathHint', { path: koreaderStatus?.base_path || '/koreader' })}</p>
            <FieldErrors messages={koreaderFieldErrors('koreader.base_path')} />
          </div>

          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.koreader.allowRegistration')}</label>
            <select
              value={koreaderForm.allow_registration ? 'true' : 'false'}
              onChange={(e) => setKOReaderForm({ ...koreaderForm, allow_registration: e.target.value === 'true' })}
              className={inputClassName}
            >
              <option value="false">{t('settings.koreader.off')}</option>
              <option value="true">{t('settings.koreader.on')}</option>
            </select>
            <p className="mt-1 text-xs text-gray-500">{t('settings.koreader.allowRegistrationHint')}</p>
          </div>

          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.koreader.matchMode')}</label>
            <select value={koreaderForm.match_mode} onChange={(e) => setKOReaderForm({ ...koreaderForm, match_mode: e.target.value })} className={inputClassName}>
              <option value="binary_hash">{t('task.koreader.binaryHashIndex')}</option>
              <option value="file_path">{t('task.koreader.pathIndex')}</option>
            </select>
            <p className="mt-1 text-xs text-gray-500">{t('settings.koreader.matchModeHint', { depth: koreaderStatus?.path_match_depth ?? 2 })}</p>
            <FieldErrors messages={koreaderFieldErrors('koreader.match_mode')} />
          </div>

          <div className="rounded-xl border border-gray-800 bg-gray-900/50 p-4">
            <label className="flex items-center justify-between gap-3">
              <div>
                <p className="text-sm font-medium text-white">{t('settings.koreader.ignoreExtension')}</p>
                <p className="mt-1 text-xs text-gray-500">{t('settings.koreader.ignoreExtensionHint')}</p>
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

        <div className="rounded-xl border border-komgaSecondary/20 bg-komgaSecondary/10 p-4 text-sm text-komgaSecondary">
          <p className="font-medium">{t('settings.koreader.setupTitle')}</p>
          <p className="mt-1 opacity-80">
            {t('settings.koreader.setupDescription', { url: `${window.location.origin}${koreaderStatus?.base_path || '/koreader'}` })}
          </p>
        </div>

        {needsMatchingMaintenance && (
          <div className="rounded-xl border border-komgaPrimary/20 bg-komgaPrimary/10 p-4 text-sm text-komgaPrimary">
            <p className="font-medium">{t('settings.koreader.matchingChangedTitle')}</p>
            <p className="mt-1 opacity-80">{t('settings.koreader.matchingChangedDescription')}</p>
            <button onClick={handleApplyMatchingChanges} disabled={applyingMatching} className="mt-4 inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-gray-900 px-4 py-2 text-sm text-gray-200 hover:bg-gray-800 disabled:opacity-60 transition-colors">
              <RefreshCw className={`h-4 w-4 ${applyingMatching ? 'animate-spin' : ''}`} />
              {applyingMatching ? t('settings.koreader.submitting') : t('settings.koreader.applyMatching')}
            </button>
          </div>
        )}

        <div className="grid gap-3 md:grid-cols-3">
          <button onClick={saveKOReader} disabled={savingKOReader} className="rounded-xl border border-gray-800 bg-gray-900/50 px-4 py-4 text-left text-gray-300 hover:text-white hover:bg-gray-800 disabled:opacity-60 transition-colors">
            <p className="inline-flex items-center gap-2 font-medium">
              <Save className={`h-4 w-4 ${savingKOReader ? 'animate-spin' : ''}`} />
              {savingKOReader ? t('settings.koreader.saving') : t('settings.koreader.save')}
            </p>
            <p className="mt-1 text-xs opacity-70">{t('settings.koreader.saveHint')}</p>
          </button>
          <button onClick={() => handleAction('/api/system/koreader/rebuild-hashes', t('settings.koreader.rebuildHashesSuccess'), t('settings.koreader.rebuildHashesFailed'))} className="rounded-xl border border-gray-800 bg-gray-900/50 px-4 py-4 text-left text-gray-300 hover:text-white hover:bg-gray-800 transition-colors">
            <p className="font-medium">{t('settings.koreader.rebuildHashes', { label: formatKOReaderIndexLabel(koreaderForm.match_mode, koreaderForm.path_ignore_extension) })}</p>
            <p className="mt-1 text-xs opacity-70">{t('settings.koreader.rebuildHashesHint')}</p>
          </button>
          <button onClick={() => handleAction('/api/system/koreader/reconcile', t('settings.koreader.reconcileSuccess'), t('settings.koreader.reconcileFailed'))} className="rounded-xl border border-gray-800 bg-gray-900/50 px-4 py-4 text-left text-gray-300 hover:text-white hover:bg-gray-800 transition-colors">
            <p className="font-medium">{t('settings.koreader.reconcile')}</p>
            <p className="mt-1 text-xs opacity-70">{t('settings.koreader.reconcileHint')}</p>
          </button>
        </div>
      </section>

      <section className={sectionClassName}>
        <h3 className="text-lg font-semibold text-white">{t('settings.koreader.accountsTitle')}</h3>
        <div className="flex flex-col gap-3 md:flex-row md:items-end">
          <div className="flex-1">
            <label className="mb-1 block text-sm text-gray-400">{t('settings.koreader.newAccount')}</label>
            <input type="text" value={koreaderAccountForm.username} onChange={(e) => setKOReaderAccountForm({ username: e.target.value })} className={inputClassName} placeholder={t('settings.koreader.newAccountPlaceholder')} />
            <FieldErrors messages={koreaderFieldErrors('koreader.accounts.username')} />
          </div>
          <button onClick={handleCreateKOReaderAccount} disabled={creatingAccount || !koreaderAccountForm.username.trim()} className="inline-flex items-center justify-center gap-2 rounded-lg border border-komgaPrimary/30 bg-komgaPrimary/10 px-4 py-2.5 text-sm text-komgaPrimary hover:bg-komgaPrimary/20 disabled:opacity-60 transition-colors">
            {creatingAccount ? <RefreshCw className="h-4 w-4 animate-spin" /> : <UserPlus className="h-4 w-4" />}
            {creatingAccount ? t('settings.koreader.creatingAccount') : t('settings.koreader.createAccount')}
          </button>
        </div>

        <div className="space-y-3">
          {koreaderAccounts.length === 0 ? (
            <p className="text-sm text-gray-500">{t('settings.koreader.noAccounts')}</p>
          ) : (
            koreaderAccounts.map((account) => (
              <div key={account.id} className="rounded-lg border border-gray-800 bg-black/20 p-4 space-y-3">
                <div className="flex flex-col gap-2 md:flex-row md:items-start md:justify-between">
                  <div>
                    <p className="text-sm font-medium text-white">{account.username}</p>
                    <p className="mt-1 text-xs text-gray-500">{t('settings.koreader.accountStatus', { status: account.enabled ? t('settings.koreader.enabledStatus') : t('settings.koreader.disabledStatus'), value: account.last_used_at ? formatDateTime(account.last_used_at) : t('common.none') })}</p>
                    {account.latest_error && <p className="mt-1 text-xs text-red-500">{t('settings.koreader.latestError', { error: account.latest_error })}</p>}
                  </div>
                  <div className="text-xs text-gray-500">{t('settings.koreader.createdAt', { value: formatDateTime(account.created_at) })}</div>
                </div>
                <div className="rounded-lg border border-gray-800 bg-gray-950 px-3 py-2">
                  <p className="text-[11px] uppercase tracking-wide text-gray-500">{t('settings.koreader.rawSyncKey')}</p>
                  <p className="mt-1 break-all font-mono text-sm text-komgaPrimary">{account.sync_key}</p>
                </div>
                <div className="flex flex-wrap gap-2">
                  <button onClick={() => handleCopySyncKey(account)} className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-black/20 px-3 py-2 text-xs text-gray-200 hover:bg-black/30">
                    <Copy className="h-3.5 w-3.5" />
                    {t('settings.koreader.copySyncKey')}
                  </button>
                  <button onClick={() => handleRotateKOReaderAccount(account)} disabled={accountActionId === account.id} className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-black/20 px-3 py-2 text-xs text-gray-200 hover:bg-black/30 disabled:opacity-60">
                    {accountActionId === account.id ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <RotateCcw className="h-3.5 w-3.5" />}
                    {t('settings.koreader.rotateSyncKey')}
                  </button>
                  <button onClick={() => handleToggleKOReaderAccount(account)} disabled={accountActionId === account.id} className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-black/20 px-3 py-2 text-xs text-gray-200 hover:bg-black/30 disabled:opacity-60">
                    <KeyRound className="h-3.5 w-3.5" />
                    {account.enabled ? t('settings.koreader.disableAccount') : t('settings.koreader.enableAccount')}
                  </button>
                  <button onClick={() => handleDeleteKOReaderAccount(account)} disabled={accountActionId === account.id} className="inline-flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs text-red-200 hover:bg-red-500/15 disabled:opacity-60">
                    <Trash2 className="h-3.5 w-3.5" />
                    {t('settings.koreader.deleteAccount')}
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
            <h3 className="text-lg font-semibold text-white">{t('settings.koreader.unmatchedTitle')}</h3>
            <p className="mt-1 text-sm text-gray-400">{t('settings.koreader.unmatchedDescription')}</p>
          </div>
          <button onClick={fetchKOReaderUnmatched} className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-black/20 px-3 py-2 text-xs text-gray-200 hover:bg-black/30">
            <RefreshCw className="h-3.5 w-3.5" />
            {t('common.refresh')}
          </button>
        </div>
        <div className="space-y-3">
          {unmatchedItems.length === 0 ? (
            <p className="text-sm text-gray-500">{t('settings.koreader.noUnmatched')}</p>
          ) : (
            unmatchedItems.map((item) => (
              <div key={item.id} className="rounded-lg border border-gray-800 bg-black/20 p-3">
                <div className="flex flex-col gap-2 md:flex-row md:items-start md:justify-between">
                  <div className="min-w-0">
                    <p className="text-sm font-medium text-white break-all">{item.document}</p>
                    <p className="mt-1 text-xs text-gray-500 break-all">{t('settings.koreader.currentKey', { key: item.normalized_key || t('settings.koreader.cannotNormalize') })}</p>
                  </div>
                  <div className="text-xs text-gray-500">{Math.round(item.percentage * 100)}% · {formatDateTime(item.updated_at)}</div>
                </div>
                <p className="mt-2 text-xs text-gray-400">{t('settings.koreader.device', { device: item.device || t('common.unknown') })}{item.device_id ? ` (${item.device_id})` : ''}</p>
                <p className="mt-2 text-xs text-komgaPrimary opacity-90">{item.suggestion}</p>
              </div>
            ))
          )}
        </div>
      </section>
    </div>
  );
}
