/**
 * 业务说明：本文件是首次运行的「创建首个管理员」引导页。仅当站点尚无任何账户时由 AuthGate 展示。
 * 创建的账户为管理员角色，将承接旧的全局阅读进度与 KOReader 账户（迁移逻辑在后端阶段2/3 挂接）。
 * 维护要点：密码至少 8 位并二次确认；成功后自动登录进入应用。
 */

import { useState } from 'react';
import { Loader2 } from 'lucide-react';
import { useAuth } from '../../auth/AuthProvider';
import { useI18n } from '../../i18n/LocaleProvider';
import { AuthShell, authInputClassName, authLabelClassName } from './AuthShell';

const MIN_PASSWORD = 8;

export default function SetupPage() {
  const { t } = useI18n();
  const { setup } = useAuth();
  const [username, setUsername] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;
    setError('');
    if (password.length < MIN_PASSWORD) {
      setError(t('auth.error.passwordTooShort'));
      return;
    }
    if (password !== confirm) {
      setError(t('auth.error.passwordMismatch'));
      return;
    }
    setSubmitting(true);
    try {
      await setup(username.trim(), password, displayName.trim());
    } catch (err) {
      setError(err instanceof Error ? err.message : t('auth.error.generic'));
      setSubmitting(false);
    }
  };

  return (
    <AuthShell title={t('auth.setup.title')} subtitle={t('auth.setup.subtitle')}>
      <form onSubmit={onSubmit} className="space-y-4">
        <div>
          <label className={authLabelClassName} htmlFor="setup-username">{t('auth.field.username')}</label>
          <input
            id="setup-username"
            className={authInputClassName}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
            autoFocus
            required
          />
        </div>
        <div>
          <label className={authLabelClassName} htmlFor="setup-display">{t('auth.field.displayName')}</label>
          <input
            id="setup-display"
            className={authInputClassName}
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder={t('auth.field.displayNameOptional')}
            autoComplete="nickname"
          />
        </div>
        <div>
          <label className={authLabelClassName} htmlFor="setup-password">{t('auth.field.password')}</label>
          <input
            id="setup-password"
            type="password"
            className={authInputClassName}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            required
          />
          <p className="mt-1 text-xs text-gray-500">{t('auth.field.passwordHint')}</p>
        </div>
        <div>
          <label className={authLabelClassName} htmlFor="setup-confirm">{t('auth.field.confirmPassword')}</label>
          <input
            id="setup-confirm"
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
          disabled={submitting || !username.trim() || !password || !confirm}
          className="w-full flex items-center justify-center gap-2 rounded-lg bg-komgaPrimary hover:bg-komgaPrimaryHover disabled:opacity-50 disabled:cursor-not-allowed px-4 py-2.5 font-medium text-white transition-colors"
        >
          {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
          {t('auth.setup.submit')}
        </button>
      </form>
    </AuthShell>
  );
}
