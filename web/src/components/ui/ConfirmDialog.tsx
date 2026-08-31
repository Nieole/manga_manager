import { AlertTriangle, ShieldAlert } from 'lucide-react';
import type { ReactNode } from 'react';
import { ModalShell } from './ModalShell';
import { modalGhostButtonClass, modalPrimaryButtonClass } from './modalStyles';
import { useI18n } from '../../i18n/LocaleProvider';

type ConfirmTone = 'primary' | 'warning' | 'danger';

interface ConfirmDialogProps {
  open: boolean;
  title: string;
  description?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  tone?: ConfirmTone;
  loading?: boolean;
  onClose: () => void;
  onConfirm: () => void;
  children?: ReactNode;
}

const toneMap: Record<ConfirmTone, { icon: ReactNode; buttonClass: string }> = {
  primary: {
    icon: <AlertTriangle className="h-5 w-5" />,
    buttonClass: modalPrimaryButtonClass,
  },
  warning: {
    icon: <AlertTriangle className="h-5 w-5" />,
    buttonClass:
      'inline-flex items-center justify-center gap-2 rounded-xl border border-amber-500/30 bg-amber-500/20 px-4 py-2.5 text-sm font-semibold text-amber-50 transition-all hover:bg-amber-500/30 disabled:cursor-not-allowed disabled:opacity-50',
  },
  danger: {
    icon: <ShieldAlert className="h-5 w-5" />,
    buttonClass:
      'inline-flex items-center justify-center gap-2 rounded-xl border border-red-500/30 bg-red-500/20 px-4 py-2.5 text-sm font-semibold text-red-50 transition-all hover:bg-red-500/30 disabled:cursor-not-allowed disabled:opacity-50',
  },
};

export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel,
  cancelLabel,
  tone = 'primary',
  loading = false,
  onClose,
  onConfirm,
  children,
}: ConfirmDialogProps) {
  const { t } = useI18n();
  const toneConfig = toneMap[tone];

  return (
    <ModalShell
      open={open}
      onClose={loading ? () => {} : onClose}
      title={title}
      description={description}
      icon={toneConfig.icon}
      size="compact"
      closeOnBackdrop={!loading}
      footer={
        <div className="flex flex-col-reverse justify-end gap-3 sm:flex-row">
          <button onClick={onClose} className={modalGhostButtonClass} disabled={loading}>
            {cancelLabel || t('modal.cancel')}
          </button>
          <button onClick={onConfirm} className={toneConfig.buttonClass} disabled={loading}>
            {confirmLabel || t('modal.confirm')}
          </button>
        </div>
      }
    >
      {children}
    </ModalShell>
  );
}
