/**
 * 业务说明：本文件是「强制改密」拦截页。管理员代建账户时初始密码为临时口令
 * （后端置 must_change_password=true），用户首次登录必须改密后才能进入应用。
 * 维护要点：需校验当前（临时）口令，新密码至少 8 位并二次确认；成功后 AuthProvider 刷新用户态放行。
 */

import { useState } from 'react';
import { Loader2 } from 'lucide-react';
import { useAuth } from '../../auth/AuthProvider';
import { useI18n } from '../../i18n/LocaleProvider';
import { AuthShell, authInputClassName, authLabelClassName } from './AuthShell';

const MIN_PASSWORD = 8;

export default function ForcePasswordChangePage() {
  const { t } = useI18n();
  const { changePassword, logout } = useAuth();
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;
    setError('');
    if (next.length < MIN_PASSWORD) {
      setError(t('auth.error.passwordTooShort'));
      return;
    }
    if (next !== confirm) {
      setError(t('auth.error.passwordMismatch'));
      return;
    }
    setSubmitting(true);
    try {
      await changePassword(current, next);
    } catch (err) {
      setError(err instanceof Error ? err.message : t('auth.error.generic'));
      setSubmitting(false);
    }
  };

  return (
    <AuthShell title={t('auth.forceChange.title')} subtitle={t('auth.forceChange.subtitle')}>
      <form onSubmit={onSubmit} className="space-y-4">
        <div>
          <label className={authLabelClassName} htmlFor="fc-current">{t('auth.field.currentPassword')}</label>
          <input
            id="fc-current"
            type="password"
            className={authInputClassName}
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
            autoComplete="current-password"
            autoFocus
            required
          />
        </div>
        <div>
          <label className={authLabelClassName} htmlFor="fc-new">{t('auth.field.newPassword')}</label>
          <input
            id="fc-new"
            type="password"
            className={authInputClassName}
            value={next}
            onChange={(e) => setNext(e.target.value)}
            autoComplete="new-password"
            required
          />
          <p className="mt-1 text-xs text-gray-500">{t('auth.field.passwordHint')}</p>
        </div>
        <div>
          <label className={authLabelClassName} htmlFor="fc-confirm">{t('auth.field.confirmPassword')}</label>
          <input
            id="fc-confirm"
            type="password"
            className={authInputClassName}
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            autoComplete="new-password"
            required
          />
        </div>
        {error ? <p className="text-sm text-red-400">{error}</p> : null}
        <button
          type="submit"
          disabled={submitting || !current || !next || !confirm}
          className="w-full flex items-center justify-center gap-2 rounded-lg bg-komgaPrimary hover:bg-komgaPrimaryHover disabled:opacity-50 disabled:cursor-not-allowed px-4 py-2.5 font-medium text-white transition-colors"
        >
          {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
          {t('auth.forceChange.submit')}
        </button>
        <button
          type="button"
          onClick={() => void logout()}
          className="w-full text-center text-sm text-gray-400 hover:text-gray-200 transition-colors"
        >
          {t('auth.logout')}
        </button>
      </form>
    </AuthShell>
  );
}
