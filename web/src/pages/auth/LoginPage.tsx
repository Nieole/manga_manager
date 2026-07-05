/**
 * 业务说明：本文件是登录页。校验站点用户名 + 密码，成功后由 AuthProvider 建立 Cookie 会话。
 * 账户由管理员创建（不开放自助注册），故本页只有登录表单，无注册入口。
 * 维护要点：密码框始终由用户键入，前端不缓存明文；提交期间禁用按钮避免重复请求。
 */

import { useState } from 'react';
import { Loader2 } from 'lucide-react';
import { useAuth } from '../../auth/AuthProvider';
import { useI18n } from '../../i18n/LocaleProvider';
import { AuthShell, authInputClassName, authLabelClassName } from './AuthShell';

export default function LoginPage() {
  const { t } = useI18n();
  const { login } = useAuth();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;
    setError('');
    setSubmitting(true);
    try {
      await login(username.trim(), password);
    } catch (err) {
      setError(err instanceof Error ? err.message : t('auth.error.generic'));
      setSubmitting(false);
    }
  };

  return (
    <AuthShell title={t('auth.login.title')} subtitle={t('auth.login.subtitle')}>
      <form onSubmit={onSubmit} className="space-y-4">
        <div>
          <label className={authLabelClassName} htmlFor="login-username">{t('auth.field.username')}</label>
          <input
            id="login-username"
            className={authInputClassName}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
            autoFocus
            required
          />
        </div>
        <div>
          <label className={authLabelClassName} htmlFor="login-password">{t('auth.field.password')}</label>
          <input
            id="login-password"
            type="password"
            className={authInputClassName}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </div>
        {error ? <p className="text-sm text-red-400">{error}</p> : null}
        <button
          type="submit"
          disabled={submitting || !username.trim() || !password}
          className="w-full flex items-center justify-center gap-2 rounded-lg bg-komgaPrimary hover:bg-komgaPrimaryHover disabled:opacity-50 disabled:cursor-not-allowed px-4 py-2.5 font-medium text-white transition-colors"
        >
          {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
          {t('auth.login.submit')}
        </button>
      </form>
    </AuthShell>
  );
}
