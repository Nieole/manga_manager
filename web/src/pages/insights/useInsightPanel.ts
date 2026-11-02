import { useEffect, useState } from 'react';
import { apiClient } from '../../api/client';

// Dashboard/Stats panels refresh independently. A different URL never borrows
// the previous period's values; identity is owned by each page's keyed boundary.
export function useInsightPanel<T>(url: string, refresh: unknown, enabled = true) {
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<{ url: string; data?: T; failed: boolean; pending: boolean }>({ url, failed: false, pending: true });
  useEffect(() => {
    if (!enabled) return;
    let active = true;
    setResult(previous => ({ url, data: previous.url === url ? previous.data : undefined, failed: false, pending: true }));
    apiClient.get<T>(url).then(response => {
      if (active) setResult({ url, data: response.data, failed: false, pending: false });
    }).catch(() => {
      if (active) setResult(previous => ({ ...previous, failed: true, pending: false }));
    });
    return () => { active = false; };
  }, [url, refresh, attempt, enabled]);
  const current = result.url === url ? result : { data: undefined, failed: false, pending: true };
  return { ...current, enabled, retry: () => setAttempt(value => value + 1) };
}
