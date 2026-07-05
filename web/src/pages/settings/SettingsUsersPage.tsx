/**
 * 业务说明：本文件是用户管理设置页（仅管理员可见）。列出、创建、删除站点账户，改角色、重置密码。
 * 账户体系不开放自助注册——新账户在此由管理员创建并设初始密码，用户首次登录被强制改密。
 * 维护要点：不能删除自己 / 降级或删除最后一个管理员（后端 403 兜底）；密码明文只在提交时经 HTTPS/同源发送，不缓存。
 */

import { useEffect, useMemo, useState } from 'react';
import { Loader2, ShieldCheck, Trash2, UserPlus, KeyRound } from 'lucide-react';
import { apiClient, getApiErrorMessage } from '../../api/client';
import { useI18n } from '../../i18n/LocaleProvider';
import { useToast } from '../../components/ToastProvider';
import { useAuth, type UserRole } from '../../auth/AuthProvider';
import { ConfirmDialog } from '../../components/ui/ConfirmDialog';
import { SettingsPageIntro, sectionClassName, inputClassName } from './shared';

interface UserRow {
  id: number;
  username: string;
  role: UserRole;
  display_name: string;
  must_change_password: boolean;
}

export function SettingsUsersPage() {
  const { t } = useI18n();
  const { showToast } = useToast();
  const { user: currentUser } = useAuth();

  const [users, setUsers] = useState<UserRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState({ username: '', password: '', display_name: '', role: 'regular' as UserRole });
  const [deleteTarget, setDeleteTarget] = useState<UserRow | null>(null);
  const [resetTarget, setResetTarget] = useState<UserRow | null>(null);
  const [resetPassword, setResetPassword] = useState('');
  const [busyId, setBusyId] = useState<number | null>(null);

  const load = () => {
    setLoading(true);
    apiClient
      .get<UserRow[]>('/api/users')
      .then((res) => setUsers(res.data || []))
      .catch((err) => showToast(getApiErrorMessage(err, t('settingsUsers.loadFailed')), 'error'))
      .finally(() => setLoading(false));
  };
  useEffect(load, []); // eslint-disable-line react-hooks/exhaustive-deps

  const sorted = useMemo(
    () => [...users].sort((a, b) => a.id - b.id),
    [users],
  );

  const submitCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (creating) return;
    if (!form.username.trim() || form.password.length < 8) return;
    setCreating(true);
    try {
      await apiClient.post('/api/users', {
        username: form.username.trim(),
        password: form.password,
        display_name: form.display_name.trim(),
        role: form.role,
      });
      showToast(t('settingsUsers.created'), 'success');
      setForm({ username: '', password: '', display_name: '', role: 'regular' });
      load();
    } catch (err) {
      showToast(getApiErrorMessage(err, t('settingsUsers.createFailed')), 'error');
    } finally {
      setCreating(false);
    }
  };

  const changeRole = async (u: UserRow, role: UserRole) => {
    if (u.role === role) return;
    setBusyId(u.id);
    try {
      await apiClient.patch(`/api/users/${u.id}`, { display_name: u.display_name, role });
      load();
    } catch (err) {
      showToast(getApiErrorMessage(err, t('settingsUsers.createFailed')), 'error');
    } finally {
      setBusyId(null);
    }
  };

  const submitReset = async () => {
    if (!resetTarget || resetPassword.length < 8) return;
    setBusyId(resetTarget.id);
    try {
      await apiClient.post(`/api/users/${resetTarget.id}/password`, { password: resetPassword });
      showToast(t('settingsUsers.resetSuccess'), 'success');
      setResetTarget(null);
      setResetPassword('');
      load();
    } catch (err) {
      showToast(getApiErrorMessage(err, t('settingsUsers.resetFailed')), 'error');
    } finally {
      setBusyId(null);
    }
  };

  const submitDelete = async () => {
    if (!deleteTarget) return;
    setBusyId(deleteTarget.id);
    try {
      await apiClient.delete(`/api/users/${deleteTarget.id}`);
      showToast(t('settingsUsers.deleted'), 'success');
      setDeleteTarget(null);
      load();
    } catch (err) {
      showToast(getApiErrorMessage(err, t('settingsUsers.deleteFailed')), 'error');
    } finally {
      setBusyId(null);
    }
  };

  return (
    <div className="space-y-6">
      <SettingsPageIntro title={t('settingsUsers.title')} description={t('settingsUsers.description')} />

      {/* 新建账户 */}
      <form className={sectionClassName} onSubmit={submitCreate}>
        <h2 className="flex items-center gap-2 text-base font-semibold text-white">
          <UserPlus className="h-4 w-4 text-komgaPrimary" />
          {t('settingsUsers.createTitle')}
        </h2>
        <div className="grid gap-3 sm:grid-cols-2">
          <input
            className={inputClassName}
            placeholder={t('settingsUsers.usernamePlaceholder')}
            value={form.username}
            autoComplete="off"
            onChange={(e) => setForm((f) => ({ ...f, username: e.target.value }))}
          />
          <input
            className={inputClassName}
            placeholder={t('settingsUsers.displayNamePlaceholder')}
            value={form.display_name}
            autoComplete="off"
            onChange={(e) => setForm((f) => ({ ...f, display_name: e.target.value }))}
          />
          <input
            className={inputClassName}
            type="password"
            placeholder={t('settingsUsers.passwordPlaceholder')}
            value={form.password}
            autoComplete="new-password"
            onChange={(e) => setForm((f) => ({ ...f, password: e.target.value }))}
          />
          <select
            className={inputClassName}
            value={form.role}
            onChange={(e) => setForm((f) => ({ ...f, role: e.target.value as UserRole }))}
          >
            <option value="regular">{t('auth.role.regular')}</option>
            <option value="admin">{t('auth.role.admin')}</option>
          </select>
        </div>
        <div>
          <button
            type="submit"
            disabled={creating || !form.username.trim() || form.password.length < 8}
            className="inline-flex items-center gap-2 rounded-lg bg-komgaPrimary hover:bg-komgaPrimaryHover disabled:opacity-50 disabled:cursor-not-allowed px-4 py-2 text-sm font-medium text-white transition-colors"
          >
            {creating ? <Loader2 className="h-4 w-4 animate-spin" /> : <UserPlus className="h-4 w-4" />}
            {t('settingsUsers.create')}
          </button>
        </div>
      </form>

      {/* 账户列表 */}
      <div className={sectionClassName}>
        {loading ? (
          <div className="flex justify-center py-8">
            <Loader2 className="h-5 w-5 animate-spin text-komgaPrimary" />
          </div>
        ) : sorted.length === 0 ? (
          <p className="py-6 text-center text-sm text-gray-500">{t('settingsUsers.empty')}</p>
        ) : (
          <ul className="divide-y divide-gray-800">
            {sorted.map((u) => {
              const isSelf = currentUser?.id === u.id;
              return (
                <li key={u.id} className="flex flex-wrap items-center gap-3 py-3">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate font-medium text-white">{u.display_name || u.username}</span>
                      {u.role === 'admin' ? <ShieldCheck className="h-3.5 w-3.5 text-komgaPrimary" /> : null}
                      {isSelf ? <span className="text-xs text-gray-500">{t('settingsUsers.you')}</span> : null}
                    </div>
                    <div className="mt-0.5 flex items-center gap-2 text-xs text-gray-500">
                      <span>@{u.username}</span>
                      {u.must_change_password ? <span className="text-amber-400">· {t('settingsUsers.mustChange')}</span> : null}
                    </div>
                  </div>

                  <select
                    className="rounded-lg border border-gray-800 bg-gray-900 px-2.5 py-1.5 text-sm text-white disabled:opacity-50"
                    value={u.role}
                    disabled={busyId === u.id}
                    onChange={(e) => changeRole(u, e.target.value as UserRole)}
                    aria-label={t('settingsUsers.role')}
                  >
                    <option value="regular">{t('auth.role.regular')}</option>
                    <option value="admin">{t('auth.role.admin')}</option>
                  </select>

                  <button
                    type="button"
                    onClick={() => { setResetTarget(u); setResetPassword(''); }}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-gray-800 px-2.5 py-1.5 text-sm text-gray-300 hover:bg-gray-800 hover:text-white transition-colors"
                    title={t('settingsUsers.resetPassword')}
                  >
                    <KeyRound className="h-3.5 w-3.5" />
                  </button>

                  <button
                    type="button"
                    disabled={isSelf}
                    onClick={() => setDeleteTarget(u)}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-red-500/20 px-2.5 py-1.5 text-sm text-red-300 hover:bg-red-500/10 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
                    title={t('settingsUsers.delete')}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>

      {/* 重置密码 */}
      <ConfirmDialog
        open={resetTarget !== null}
        onClose={() => { setResetTarget(null); setResetPassword(''); }}
        onConfirm={submitReset}
        title={t('settingsUsers.resetTitle')}
        confirmLabel={t('settingsUsers.resetPassword')}
        tone="warning"
      >
        <input
          className={inputClassName}
          type="password"
          placeholder={t('settingsUsers.newPasswordPlaceholder')}
          value={resetPassword}
          autoComplete="new-password"
          onChange={(e) => setResetPassword(e.target.value)}
        />
      </ConfirmDialog>

      {/* 删除账户 */}
      <ConfirmDialog
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        onConfirm={submitDelete}
        title={t('settingsUsers.delete')}
        description={deleteTarget ? t('settingsUsers.deleteConfirm', { name: deleteTarget.display_name || deleteTarget.username }) : ''}
        confirmLabel={t('settingsUsers.delete')}
        tone="danger"
      />
    </div>
  );
}
