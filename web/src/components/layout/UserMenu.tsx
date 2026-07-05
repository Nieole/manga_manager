/**
 * 业务说明：本文件是顶栏的账户菜单：显示当前登录用户，提供「修改密码」与「退出登录」。
 * 修改密码走 AuthProvider.changePassword（校验当前密码 → 更新 → 失效旧会话并重建当前会话）。
 * 维护要点：密码明文只在提交时发送，不入任何本地状态之外的存储；下拉点击外部即关闭。
 */

import { useState } from 'react';
import { LogOut, KeyRound, UserCircle2, ShieldCheck } from 'lucide-react';
import { useAuth } from '../../auth/AuthProvider';
import { useI18n } from '../../i18n/LocaleProvider';
import { useToast } from '../ToastProvider';
import { ConfirmDialog } from '../ui/ConfirmDialog';

const inputClass =
  'w-full bg-gray-900 border border-gray-800 rounded-lg px-3 py-2 text-sm text-white placeholder:text-gray-500 focus:outline-hidden focus:ring-2 focus:ring-komgaPrimary/40 transition-all';

export function UserMenu() {
  const { t } = useI18n();
  const { showToast } = useToast();
  const { user, isAdmin, logout, changePassword } = useAuth();

  const [open, setOpen] = useState(false);
  const [pwOpen, setPwOpen] = useState(false);
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [saving, setSaving] = useState(false);

  if (!user) return null;

  const submitChange = async () => {
    if (next.length < 8) {
      showToast(t('auth.error.passwordTooShort'), 'error');
      return;
    }
    if (next !== confirm) {
      showToast(t('auth.error.passwordMismatch'), 'error');
      return;
    }
    setSaving(true);
    try {
      await changePassword(current, next);
      showToast(t('auth.changePassword.success'), 'success');
      setPwOpen(false);
      setCurrent(''); setNext(''); setConfirm('');
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('auth.error.generic'), 'error');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="p-2 text-gray-400 hover:text-komgaPrimary hover:bg-gray-800 rounded-full transition-colors"
        title={t('auth.account')}
        aria-label={t('auth.account')}
      >
        <UserCircle2 className="w-6 h-6" />
      </button>

      {open ? (
        <>
          <div className="fixed inset-0 z-30" onClick={() => setOpen(false)} />
          <div className="absolute right-0 mt-2 w-56 z-40 rounded-xl border border-gray-800 bg-komgaSurface shadow-lg shadow-black/30 py-2">
            <div className="px-4 py-2 border-b border-gray-800">
              <div className="flex items-center gap-1.5">
                <span className="truncate font-medium text-white">{user.display_name || user.username}</span>
                {isAdmin ? <ShieldCheck className="h-3.5 w-3.5 text-komgaPrimary shrink-0" /> : null}
              </div>
              <div className="mt-0.5 text-xs text-gray-500">
                @{user.username} · {isAdmin ? t('auth.role.admin') : t('auth.role.regular')}
              </div>
            </div>
            <button
              type="button"
              onClick={() => { setOpen(false); setPwOpen(true); }}
              className="w-full flex items-center gap-2 px-4 py-2 text-sm text-gray-300 hover:bg-gray-800 hover:text-white transition-colors"
            >
              <KeyRound className="h-4 w-4" />
              {t('auth.changePassword')}
            </button>
            <button
              type="button"
              onClick={() => { setOpen(false); void logout(); }}
              className="w-full flex items-center gap-2 px-4 py-2 text-sm text-gray-300 hover:bg-gray-800 hover:text-white transition-colors"
            >
              <LogOut className="h-4 w-4" />
              {t('auth.logout')}
            </button>
          </div>
        </>
      ) : null}

      <ConfirmDialog
        open={pwOpen}
        onClose={() => { setPwOpen(false); setCurrent(''); setNext(''); setConfirm(''); }}
        onConfirm={submitChange}
        title={t('auth.changePassword')}
        confirmLabel={t('auth.changePassword')}
        loading={saving}
      >
        <div className="space-y-3">
          <input
            className={inputClass}
            type="password"
            placeholder={t('auth.field.currentPassword')}
            value={current}
            autoComplete="current-password"
            onChange={(e) => setCurrent(e.target.value)}
          />
          <input
            className={inputClass}
            type="password"
            placeholder={t('auth.field.newPassword')}
            value={next}
            autoComplete="new-password"
            onChange={(e) => setNext(e.target.value)}
          />
          <input
            className={inputClass}
            type="password"
            placeholder={t('auth.field.confirmPassword')}
            value={confirm}
            autoComplete="new-password"
            onChange={(e) => setConfirm(e.target.value)}
          />
        </div>
      </ConfirmDialog>
    </div>
  );
}
