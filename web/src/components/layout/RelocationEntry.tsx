import { lazy, Suspense } from 'react';
import { useLocation, useMatch, useNavigate } from 'react-router-dom';
import { useI18n } from '../../i18n/LocaleProvider';
import type { Library } from './types';

const RelocationModal = lazy(() => import('./RelocationModal').then((module) => ({ default: module.RelocationModal })));
const RestorationModal = lazy(() => import('./RestorationModal').then((module) => ({ default: module.RestorationModal })));

interface Props {
  libraries: Library[];
  isAdmin: boolean;
  selectedID: string | null;
  initialTarget?: string;
  onCompleted?: () => void;
  onClose: () => void;
}

export function RelocationEntry({ libraries, isAdmin, selectedID, initialTarget, onCompleted, onClose }: Props) {
  const location = useLocation();
  const match = useMatch('/library/:libId');
  const navigate = useNavigate();
  const { t } = useI18n();
  const params = new URLSearchParams(location.search);
  const requestedPlan = params.get('relocation_plan');
  const requestedRestore = params.get('restoration_plan');
  const restoration = Boolean(requestedRestore || params.get('restoration') === '1');
  const requested = restoration ? requestedRestore : requestedPlan;
  const planID = requested && /^[1-9]\d*$/.test(requested) && Number.isSafeInteger(Number(requested)) ? Number(requested) : undefined;
  const libraryID = planID || restoration ? match?.params.libId : selectedID;
  const library = libraries.find((item) => String(item.id) === libraryID);
  if (!isAdmin || !library) return null;

  const close = () => {
    onClose();
    if (requestedPlan || restoration) {
      params.delete('relocation_plan');
      params.delete('restoration_plan');
      params.delete('restoration');
      const query = params.toString();
      navigate({ pathname: location.pathname, search: query ? `?${query}` : '', hash: location.hash }, { replace: true });
    }
  };
  return <Suspense fallback={<p role="status">{t('common.loading')}</p>}>
    {restoration ? <RestorationModal key={`restore:${library.id}:${planID || 'latest'}`} library={{ ...library, id: Number(library.id) }} planID={planID} onCompleted={onCompleted} onClose={close} />
      : <RelocationModal key={`${library.id}:${planID || 'latest'}`} library={{ ...library, id: Number(library.id) }} planID={planID} initialTarget={planID ? undefined : initialTarget} onCompleted={onCompleted} onClose={close} />}
  </Suspense>;
}
