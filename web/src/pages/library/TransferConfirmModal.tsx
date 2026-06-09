/**
 * 业务说明：本文件是业务实现，属于前端资料库页面，负责漫画列表、筛选排序、批量操作、扫描入口和外部库状态展示。
 * 它是用户管理本地漫画资产的主工作台，需要同步 URL 状态、后端分页和本地交互状态。
 * 维护时应关注查询参数、选择状态、空结果提示、任务刷新和大列表渲染性能。
 */

import { Loader2, PackageCheck, Send } from 'lucide-react';

import { ModalShell } from '../../components/ui/ModalShell';
import { modalGhostButtonClass, modalPrimaryButtonClass } from '../../components/ui/modalStyles';
import { useI18n } from '../../i18n/LocaleProvider';

export interface TransferSummary {
  total: number;
  matched: number;
  missing: number;
}

interface TransferConfirmModalProps {
  open: boolean;
  onClose: () => void;
  selectedCount: number;
  externalPath?: string;
  summary: TransferSummary | null;
  submitting: boolean;
  onConfirm: () => void;
}

export function TransferConfirmModal({
  open,
  onClose,
  selectedCount,
  externalPath,
  summary,
  submitting,
  onConfirm,
}: TransferConfirmModalProps) {
  const { t } = useI18n();

  const handleClose = () => {
    if (submitting) return;
    onClose();
  };

  return (
    <ModalShell
      open={open}
      onClose={handleClose}
      title={t('home.transfer.title')}
      description={t('home.transfer.description')}
      icon={<PackageCheck className="h-5 w-5" />}
      size="compact"
      closeOnBackdrop={!submitting}
      footer={
        <div className="flex flex-col-reverse justify-end gap-3 sm:flex-row">
          <button onClick={handleClose} className={modalGhostButtonClass} disabled={submitting}>
            {t('modal.cancel')}
          </button>
          <button onClick={onConfirm} className={modalPrimaryButtonClass} disabled={submitting}>
            {submitting ? (
              <>
                <Loader2 className="h-4 w-4 animate-spin" />
                {t('home.transfer.submitting')}
              </>
            ) : (
              <>
                <Send className="h-4 w-4" />
                {t('home.transfer.confirm')}
              </>
            )}
          </button>
        </div>
      }
    >
      <div className="space-y-4">
        <div className="rounded-2xl border border-gray-800 bg-gray-950/60 p-4">
          <p className="text-sm text-gray-300 leading-6">
            {t('home.transfer.summary', { count: selectedCount })}
          </p>
          <p className="mt-2 break-all text-xs text-gray-500">{externalPath}</p>
        </div>
        {summary && (
          <div className="grid grid-cols-3 gap-3 text-xs">
            <div className="rounded-lg border border-emerald-500/20 bg-emerald-500/10 px-3 py-2 text-emerald-300">
              <p>{t('home.external.matched')}</p>
              <p className="mt-1 text-base font-semibold text-white">{summary.matched}</p>
            </div>
            <div className="rounded-lg border border-amber-500/20 bg-amber-500/10 px-3 py-2 text-amber-300">
              <p>{t('home.transfer.missing')}</p>
              <p className="mt-1 text-base font-semibold text-white">{summary.missing}</p>
            </div>
            <div className="rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-gray-300">
              <p>{t('home.transfer.total')}</p>
              <p className="mt-1 text-base font-semibold text-white">{summary.total}</p>
            </div>
          </div>
        )}
      </div>
    </ModalShell>
  );
}
