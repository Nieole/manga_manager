import { useI18n } from '../../i18n/LocaleProvider';
import { useSettings } from './SettingsContext';

export function SettingsPublicationNotice() {
  const { t } = useI18n();
  const { fieldEffects = [], configPublication, lastPublication } = useSettings();
  const restart = configPublication?.restart_required ?? lastPublication?.restart_required ?? [];
  if (!fieldEffects.length && !lastPublication && !restart.length) return null;
  const groups = [
    { scope: 'immediate', fields: lastPublication?.immediate ?? [] },
    { scope: 'next_operation', fields: lastPublication?.next_operation ?? [] },
    { scope: 'restart', fields: restart },
  ];
  return <section className="mb-5 rounded-2xl border border-gray-800 bg-komgaSurface p-4" aria-label={t('settings.publication.title')}>
    {lastPublication?.saved && <p className="text-sm text-emerald-300">{t('settings.publication.saved')}</p>}
    {groups.filter(group => group.fields.length).map(group => <div key={group.scope} className="mt-2 text-sm">
      <span className={group.scope === 'restart' ? 'text-amber-300' : 'text-gray-300'}>{t(`settings.publication.${group.scope === 'restart' ? 'pendingRestart' : group.scope}`)}</span>
      <p className="mt-1 break-words text-xs text-gray-400">{group.fields.join(', ')}</p>
    </div>)}
    {fieldEffects.length > 0 && <details className="mt-2 text-xs text-gray-400">
      <summary className="cursor-pointer text-gray-300">{t('settings.publication.catalog')}</summary>
      <p className="my-2 leading-5">{t('settings.publication.description')}</p>
      <table className="w-full text-left"><tbody>{fieldEffects.map(effect => <tr key={effect.field} className="border-t border-gray-800">
        <td className="break-all py-1 pr-3"><code>{effect.field}</code></td><td className="py-1">{t(`settings.publication.${effect.scope}`)}</td>
      </tr>)}</tbody></table>
    </details>}
  </section>;
}
