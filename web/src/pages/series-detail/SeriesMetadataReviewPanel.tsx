import { CheckCircle2, ExternalLink, GitCompareArrows, ShieldCheck, XCircle } from 'lucide-react';
import type { MetadataProvenance, MetadataReview } from './types';
import { useI18n } from '../../i18n/LocaleProvider';

interface SeriesMetadataReviewPanelProps {
  reviews: MetadataReview[];
  provenance: MetadataProvenance[];
  busyReviewId: number | null;
  onApply: (reviewId: number) => void;
  onReject: (reviewId: number) => void;
}

function percent(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0%';
  return `${Math.round(Math.min(1, value) * 100)}%`;
}

export function SeriesMetadataReviewPanel({
  reviews,
  provenance,
  busyReviewId,
  onApply,
  onReject,
}: SeriesMetadataReviewPanelProps) {
  const { t, formatDateTime } = useI18n();

  if (reviews.length === 0 && provenance.length === 0) return null;

  return (
    <section className="relative z-20 mb-8 rounded-2xl border border-cyan-400/15 bg-gray-950/75 p-5 shadow-xl backdrop-blur-md">
      <div className="mb-5 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h3 className="flex items-center gap-2 text-lg font-semibold text-white">
            <GitCompareArrows className="h-5 w-5 text-cyan-300" />
            {t('series.metadataReview.title')}
          </h3>
          <p className="mt-1 text-sm text-gray-400">{t('series.metadataReview.description')}</p>
        </div>
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          {reviews.length > 0 && (
            <span className="rounded-full border border-cyan-400/20 bg-cyan-400/10 px-3 py-1 text-xs font-medium text-cyan-200">
              {t('series.metadataReview.pendingCount', { count: reviews.length })}
            </span>
          )}
        </div>
      </div>

      {reviews.length > 0 && (
        <div className="space-y-4">
          {reviews.map((review) => (
            <div key={review.id} className="rounded-2xl border border-white/10 bg-black/25 p-4">
              <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="rounded-lg border border-cyan-400/20 bg-cyan-400/10 px-2.5 py-1 text-xs font-semibold text-cyan-200">
                      {review.provider || t('series.metadataReview.unknownSource')}
                    </span>
                    <span className="text-xs text-gray-500">{formatDateTime(review.created_at)}</span>
                    <span className="text-xs text-gray-500">{t('series.metadataReview.confidence', { value: percent(review.confidence) })}</span>
                  </div>
                  <p className="mt-2 text-sm text-gray-300">{review.summary}</p>
                  {review.source_url && (
                    <a
                      href={review.source_url}
                      target="_blank"
                      rel="noreferrer"
                      className="mt-2 inline-flex max-w-full items-center gap-1.5 truncate text-xs text-cyan-300 hover:text-cyan-200"
                    >
                      <ExternalLink className="h-3.5 w-3.5 shrink-0" />
                      <span className="truncate">{review.source_url}</span>
                    </a>
                  )}
                </div>
                <div className="flex shrink-0 gap-2">
                  <button
                    onClick={() => onReject(review.id)}
                    disabled={busyReviewId === review.id}
                    className="inline-flex items-center justify-center gap-2 rounded-xl border border-red-400/20 bg-red-500/10 px-3 py-2 text-sm font-medium text-red-200 transition-colors hover:bg-red-500/15 disabled:opacity-50"
                  >
                    <XCircle className="h-4 w-4" />
                    {t('series.metadataReview.reject')}
                  </button>
                  <button
                    onClick={() => onApply(review.id)}
                    disabled={busyReviewId === review.id}
                    className="inline-flex items-center justify-center gap-2 rounded-xl bg-komgaPrimary px-3 py-2 text-sm font-semibold text-white shadow-lg shadow-komgaPrimary/20 transition-colors hover:bg-komgaPrimaryHover disabled:opacity-50"
                  >
                    <CheckCircle2 className="h-4 w-4" />
                    {t('series.metadataReview.apply')}
                  </button>
                </div>
              </div>

              <div className="mt-4 grid gap-3">
                {review.fields.map((field) => (
                  <div key={field.name} className="rounded-xl border border-white/10 bg-gray-950/70 p-3">
                    <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
                      <span className="text-sm font-semibold text-white">{field.label}</span>
                      <div className="flex items-center gap-2">
                        {field.locked && (
                          <span className="inline-flex items-center gap-1 rounded-full border border-amber-400/20 bg-amber-400/10 px-2 py-1 text-[11px] font-medium text-amber-500">
                            <ShieldCheck className="h-3 w-3" />
                            {t('series.metadataReview.locked')}
                          </span>
                        )}
                        <span className="rounded-full border border-white/10 bg-white/5 px-2 py-1 text-[11px] text-gray-400">
                          {percent(field.confidence)}
                        </span>
                      </div>
                    </div>
                    <div className="grid gap-3 md:grid-cols-2">
                      <div className="min-w-0">
                        <p className="mb-1 text-[11px] uppercase tracking-[0.16em] text-gray-500">{t('series.metadataReview.current')}</p>
                        <div className="min-h-10 rounded-lg border border-white/10 bg-black/20 px-3 py-2 text-sm text-gray-400 whitespace-pre-wrap wrap-break-word">
                          {field.current || t('common.none')}
                        </div>
                      </div>
                      <div className="min-w-0">
                        <p className="mb-1 text-[11px] uppercase tracking-[0.16em] text-gray-500">{t('series.metadataReview.proposed')}</p>
                        <div className="min-h-10 rounded-lg border border-cyan-400/15 bg-cyan-400/5 px-3 py-2 text-sm text-gray-100 whitespace-pre-wrap wrap-break-word">
                          {field.proposed || t('common.none')}
                        </div>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}

      {provenance.length > 0 && (
        <div className="mt-5 rounded-2xl border border-white/10 bg-white/3 p-4">
          <h4 className="mb-3 text-sm font-semibold text-gray-100">{t('series.metadataReview.provenanceTitle')}</h4>
          <div className="flex flex-wrap gap-2">
            {provenance.map((row) => (
              <span key={row.field_name} className="inline-flex max-w-full items-center gap-2 rounded-xl border border-white/10 bg-gray-950/70 px-3 py-2 text-xs text-gray-300">
                <span className="font-semibold text-white">{row.label}</span>
                <span className="text-gray-500">{row.source || t('series.metadataReview.unknownSource')}</span>
                <span className="text-gray-600">{percent(row.confidence)}</span>
              </span>
            ))}
          </div>
        </div>
      )}
    </section>
  );
}
