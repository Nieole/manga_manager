import { lazy, Suspense } from 'react';
import { useLocation, useMatch, useNavigate } from 'react-router-dom';
import { useI18n } from '../../i18n/LocaleProvider';
import type { Library } from './types';

const RelocationModal = lazy(() => import('./RelocationModal').then((module) => ({ default: module.RelocationModal })));

interface Props {
  libraries: Library[];
  isAdmin: boolean;
  selectedID: string | null;
  onClose: () => void;
}

export function RelocationEntry({ libraries, isAdmin, selectedID, onClose }: Props) {
  const location = useLocation();
  const match = useMatch('/library/:libId');
  const navigate = useNavigate();
  const { t } = useI18n();
  const params = new URLSearchParams(location.search);
  const requestedPlan = params.get('relocation_plan');
  const planID = requestedPlan && /^[1-9]\d*$/.test(requestedPlan) && Number.isSafeInteger(Number(requestedPlan)) ? Number(requestedPlan) : undefined;
  const libraryID = planID ? match?.params.libId : selectedID;
  const library = libraries.find((item) => String(item.id) === libraryID);
  if (!isAdmin || !library) return null;

  const close = () => {
    onClose();
    if (requestedPlan) {
      params.delete('relocation_plan');
      const query = params.toString();
      navigate({ pathname: location.pathname, search: query ? `?${query}` : '', hash: location.hash }, { replace: true });
    }
  };
  return <Suspense fallback={<p role="status">{t('common.loading')}</p>}>
    <RelocationModal key={`${library.id}:${planID || 'latest'}`} library={{ ...library, id: Number(library.id) }} planID={planID} onClose={close} />
  </Suspense>;
}
