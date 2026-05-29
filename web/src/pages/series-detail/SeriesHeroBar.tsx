import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import {
  ArrowLeft,
  BookImage,
  BookOpen,
  Building2,
  ChevronDown,
  ChevronUp,
  ExternalLink,
  Globe,
  Play,
  Sparkles,
  Star,
  Tag,
} from 'lucide-react';
import type { Author, MetaTag, Series, SeriesContinue, SeriesLink as SeriesLinkType } from './types';
import type { ContinueCta } from './hooks/useSeriesContinue';
import { normalizeSeriesStatus } from '../../i18n/status';
import { useI18n } from '../../i18n/LocaleProvider';

interface SeriesHeroBarProps {
  series: Series | null;
  tags: MetaTag[];
  authors: Author[];
  links: SeriesLinkType[];
  continueInfo: SeriesContinue | null;
  continueCta: ContinueCta | null;
  bookCount: number;
  volumeCount: number;
  standaloneCount: number;
  coverUrl: string | null;
  onBack: () => void;
  selectionToggle: { isSelectionMode: boolean; toggle: () => void };
  rightSlot?: React.ReactNode;
  badgeSlot?: React.ReactNode;
}

function libraryFilterTo(libraryId: number | undefined, key: 'tag' | 'author', value: string) {
  if (!libraryId) return '#';
  const params = new URLSearchParams();
  params.set(key, value);
  return `/library/${libraryId}?${params.toString()}`;
}

function statusDotColor(status: string) {
  const normalized = normalizeSeriesStatus(status);
  if (normalized === 'ongoing') return 'bg-emerald-400 shadow-emerald-400/50';
  if (normalized === 'completed') return 'bg-sky-400 shadow-sky-400/50';
  if (normalized === 'hiatus') return 'bg-amber-400 shadow-amber-400/50';
  if (normalized === 'cancelled') return 'bg-rose-400 shadow-rose-400/50';
  return 'bg-gray-400 shadow-gray-400/40';
}

export function SeriesHeroBar({
  series,
  tags,
  authors,
  links,
  continueInfo,
  continueCta,
  bookCount,
  volumeCount,
  standaloneCount,
  coverUrl,
  onBack,
  selectionToggle,
  rightSlot,
  badgeSlot,
}: SeriesHeroBarProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [tagsExpanded, setTagsExpanded] = useState(false);

  const title = series?.title?.Valid && series.title.String ? series.title.String : series?.name || t('series.header.seriesOverview');
  const status = series?.status?.Valid ? series.status.String : '';
  const statusLabel = status ? t(`status.${normalizeSeriesStatus(status)}`) : '';
  const rating = series?.rating?.Valid ? series.rating.Float64 : 0;
  const language = series?.language?.Valid ? series.language.String : '';
  const publisher = series?.publisher?.Valid ? series.publisher.String : '';

  const totalPages = continueInfo?.total_pages ?? 0;
  const readPages = continueInfo?.read_pages ?? 0;
  const progressPct = totalPages > 0 ? Math.min(100, Math.round((readPages / totalPages) * 100)) : 0;
  const hasFinished = continueInfo && continueInfo.total_books > 0 && continueInfo.read_books >= continueInfo.total_books;

  const ctaLabel = continueCta
    ? hasFinished
      ? t('series.continue.reread', { book: continueCta.bookLabel })
      : continueCta.page > 0
        ? t('series.continue.resume', { volume: continueCta.volumeLabel || continueCta.bookLabel, page: continueCta.page, total: continueCta.totalPages || '?' })
        : t('series.continue.start', { book: continueCta.bookLabel })
    : null;

  return (
    <div className="mb-8 relative z-10">
      {/* 顶栏：返回 / 工具组 */}
      <div className="flex flex-wrap items-center justify-between gap-2 mb-6 relative z-20">
        <button
          onClick={onBack}
          className="inline-flex items-center gap-1.5 text-sm font-medium text-gray-300/90 hover:text-white transition-colors group"
        >
          <span className="flex h-8 w-8 items-center justify-center rounded-full bg-white/5 border border-white/10 group-hover:bg-white/15 group-hover:border-white/20 transition-colors">
            <ArrowLeft className="w-4 h-4" />
          </span>
          <span>{t('series.header.backToLibrary')}</span>
        </button>
        <div className="flex flex-wrap items-center gap-2">
          {badgeSlot}
          <button
            onClick={selectionToggle.toggle}
            className={`px-3.5 py-2 text-xs font-semibold rounded-full border transition-all ${
              selectionToggle.isSelectionMode
                ? 'bg-komgaPrimary text-white border-komgaPrimary shadow-lg shadow-komgaPrimary/30'
                : 'bg-white/5 text-gray-200 border-white/10 hover:bg-white/15 hover:border-white/20'
            }`}
          >
            {selectionToggle.isSelectionMode ? t('series.header.exitSelection') : t('series.header.enterSelection')}
          </button>
          {rightSlot}
        </div>
      </div>

      {/* Hero 主体卡片 */}
      <div className="relative">
        {/* 封面发光晕（仅桌面） */}
        {coverUrl && (
          <div
            className="hidden lg:block absolute -top-4 -left-4 w-72 h-96 rounded-3xl blur-3xl opacity-40 pointer-events-none"
            style={{
              backgroundImage: `url("${coverUrl}")`,
              backgroundSize: 'cover',
              backgroundPosition: 'center',
            }}
          />
        )}

        <div className="relative grid grid-cols-1 sm:grid-cols-[auto_1fr] gap-8 lg:gap-12 items-end sm:items-start pt-8 sm:pt-12">
          {/* Cover Art */}
          <div className="relative mx-auto sm:mx-0 shrink-0">
            <div className="relative w-40 sm:w-56 lg:w-64 transition-transform duration-500 hover:scale-[1.02]">
              <div className="absolute -inset-4 rounded-[2rem] bg-gradient-to-br from-komgaPrimary/30 to-komgaSecondary/30 blur-2xl opacity-70 group-hover:opacity-100 transition-opacity" />
              <div className="relative rounded-2xl overflow-hidden border border-white/10 bg-gray-900/50 shadow-[0_20px_60px_-15px_rgba(0,0,0,0.8)] ring-1 ring-white/5 aspect-[2/3]">
                {coverUrl ? (
                  <img src={coverUrl} alt={t('common.cover')} className="w-full h-full object-cover" />
                ) : (
                  <div className="w-full h-full flex items-center justify-center bg-gray-900/50 backdrop-blur">
                    <BookImage className="w-16 h-16 text-gray-600 opacity-50" />
                  </div>
                )}
              </div>
            </div>
          </div>

          {/* Metadata Block */}
          <div className="min-w-0 w-full flex flex-col justify-end pt-4 sm:pt-0">
            {/* Title & Status */}
            <div className="mb-6 space-y-3 text-center sm:text-left">
              <div className="flex flex-wrap items-center justify-center sm:justify-start gap-3">
                {statusLabel && (
                  <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-gray-950/40 border border-white/10 backdrop-blur-md text-[11px] font-bold tracking-widest text-white shadow-xl shadow-black/50">
                    <span className={`relative flex h-2 w-2 rounded-full ${statusDotColor(status)} shadow-[0_0_10px]`}>
                      <span className={`absolute inset-0 rounded-full ${statusDotColor(status).split(' ')[0]} animate-ping opacity-60`} />
                    </span>
                    <span className="uppercase text-white/90">{statusLabel}</span>
                  </span>
                )}
                {authors.length > 0 && (
                  <span className="inline-flex items-center flex-wrap gap-1 text-sm font-medium text-gray-300">
                    {authors.slice(0, 3).map((a, i) => (
                      <button
                        key={a.id}
                        type="button"
                        onClick={() => navigate(libraryFilterTo(series?.library_id, 'author', a.name))}
                        className="text-white hover:text-komgaPrimary transition-colors drop-shadow-md"
                      >
                        {a.name}
                        {i < Math.min(authors.length, 3) - 1 ? <span className="text-gray-500 mx-1.5 font-normal">/</span> : null}
                      </button>
                    ))}
                    {authors.length > 3 && <span className="text-gray-500">+{authors.length - 3}</span>}
                  </span>
                )}
              </div>

              <h1 className="font-black tracking-tight text-white leading-[1.1] text-4xl sm:text-5xl lg:text-7xl drop-shadow-2xl">
                {title}
              </h1>
            </div>

            {/* Description */}
            {series?.summary?.Valid && series.summary.String && (
              <p className="text-sm sm:text-base text-gray-200 leading-relaxed line-clamp-3 hover:line-clamp-none transition-all cursor-pointer max-w-4xl mb-8 text-center sm:text-left drop-shadow-lg">
                {series.summary.String}
              </p>
            )}

            {/* Primary Action & Progress */}
            <div className="flex flex-col sm:flex-row items-center gap-4 mb-8">
              {continueCta && (
                <Link
                  to={`/reader/${continueCta.bookId}`}
                  className="group relative inline-flex items-center justify-center gap-4 pl-2 pr-6 py-2 rounded-full bg-white text-gray-950 font-bold shadow-xl shadow-white/20 hover:shadow-2xl hover:scale-105 transition-all overflow-hidden"
                >
                  <span className="relative flex h-12 w-12 items-center justify-center rounded-full bg-gray-950/10 shrink-0">
                    <Play className="w-5 h-5 fill-current translate-x-0.5" />
                  </span>
                  <span className="relative text-base uppercase tracking-wide">{ctaLabel}</span>
                </Link>
              )}
              {totalPages > 0 && (
                <div className="flex items-center gap-4 px-5 py-3 rounded-full bg-gray-950/40 border border-white/10 backdrop-blur-xl shadow-xl shadow-black/40">
                  <ProgressRing pct={progressPct} finished={!!hasFinished} />
                  <div className="flex flex-col">
                    <span className="text-[10px] font-bold text-gray-400 uppercase tracking-widest">
                      {hasFinished ? t('series.stats.completed') : t('series.stats.progress')}
                    </span>
                    <span className="text-sm font-semibold text-white/90 tracking-wide">
                      {t('series.stats.pages', { read: readPages.toLocaleString(), total: totalPages.toLocaleString() })}
                    </span>
                  </div>
                </div>
              )}
            </div>

            {/* Stats Row */}
            <div className="flex flex-wrap items-center gap-3 mb-6 justify-center sm:justify-start">
              {rating > 0 && (
                <StatCard
                  icon={<Star className="w-4 h-4 fill-amber-400 text-amber-400" />}
                  label={t('series.stats.rating')}
                  value={rating.toFixed(1)}
                />
              )}
              <StatCard
                icon={<BookOpen className="w-4 h-4 text-indigo-400" />}
                label={t('series.stats.books', { count: bookCount })}
              />
              {volumeCount > 0 && (
                <StatCard
                  icon={<Sparkles className="w-4 h-4 text-violet-400" />}
                  label={t('series.stats.volumes', { count: volumeCount })}
                />
              )}
              {standaloneCount > 0 && (
                <StatCard
                  icon={<BookImage className="w-4 h-4 text-cyan-400" />}
                  label={t('series.stats.standalones', { count: standaloneCount })}
                />
              )}
              {language && (
                <StatCard
                  icon={<Globe className="w-4 h-4 text-emerald-400" />}
                  label={language.toUpperCase()}
                />
              )}
              {publisher && (
                <StatCard
                  icon={<Building2 className="w-4 h-4 text-rose-400" />}
                  label={publisher}
                />
              )}
            </div>

            {/* Tags 行 */}
            {tags.length > 0 && (
              <div className="flex flex-wrap items-center gap-1.5 mb-3 justify-center sm:justify-start">
                {(tagsExpanded ? tags : tags.slice(0, 10)).map((tag) => (
                  <button
                    key={tag.id}
                    type="button"
                    onClick={() => navigate(libraryFilterTo(series?.library_id, 'tag', tag.name))}
                    className="inline-flex items-center gap-1 text-xs font-medium px-2.5 py-1 rounded-full bg-komgaSecondary/10 text-komgaSecondary border border-komgaSecondary/30 hover:bg-komgaSecondary/20 hover:border-komgaSecondary/50 transition-colors max-w-full"
                    title={tag.name}
                  >
                    <Tag className="w-3 h-3 shrink-0" />
                    <span className="truncate max-w-[14rem]">{tag.name}</span>
                  </button>
                ))}
                {tags.length > 10 && (
                  <button
                    type="button"
                    onClick={() => setTagsExpanded((v) => !v)}
                    className="inline-flex items-center gap-1 text-xs font-semibold px-2.5 py-1 rounded-full bg-white/5 text-gray-200 border border-white/15 hover:bg-white/15 hover:border-white/25 transition-colors"
                    aria-expanded={tagsExpanded}
                  >
                    {tagsExpanded ? (
                      <>
                        <ChevronUp className="w-3 h-3" />
                        {t('series.tags.collapse')}
                      </>
                    ) : (
                      <>
                        <ChevronDown className="w-3 h-3" />
                        {t('series.tags.showAll', { count: tags.length - 10 })}
                      </>
                    )}
                  </button>
                )}
              </div>
            )}

            {/* 外链行 */}
            {links.length > 0 && (
              <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 justify-center sm:justify-start">
                {links.map((link) => (
                  <a
                    key={link.id}
                    href={link.url}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1.5 text-xs font-medium text-gray-400 hover:text-komgaPrimary transition-colors"
                    title={link.url}
                  >
                    <ExternalLink className="w-3 h-3 shrink-0" />
                    <span className="truncate max-w-[14rem] underline-offset-4 hover:underline">{link.name || link.url}</span>
                  </a>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}



function StatCard({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode;
  label: string;
  value?: string;
}) {
  return (
    <div
      className="inline-flex items-center gap-2 px-3.5 py-2 rounded-xl bg-gray-950/40 border border-white/10 backdrop-blur-md shadow-lg shadow-black/20 hover:bg-white/10 transition-colors"
      title={value ? `${label}: ${value}` : label}
    >
      <span className="shrink-0 drop-shadow-md">{icon}</span>
      {value ? (
        <span className="flex items-baseline gap-1.5 leading-none">
          <span className="text-sm font-extrabold text-white tracking-tight">{value}</span>
          <span className="text-[10px] font-bold uppercase tracking-wider text-gray-400">{label}</span>
        </span>
      ) : (
        <span className="text-xs font-bold leading-none text-gray-300 truncate max-w-[14rem] tracking-wide">{label}</span>
      )}
    </div>
  );
}

function ProgressRing({ pct, finished }: { pct: number; finished: boolean }) {
  const r = 16;
  const circ = 2 * Math.PI * r;
  const dash = (pct / 100) * circ;
  const color = finished ? '#34d399' : 'rgb(var(--color-komga-primary))';
  return (
    <div className="relative w-10 h-10 shrink-0">
      <svg viewBox="0 0 40 40" className="w-full h-full -rotate-90">
        <circle cx="20" cy="20" r={r} strokeWidth="3" stroke="rgba(255,255,255,0.08)" fill="none" />
        <circle
          cx="20"
          cy="20"
          r={r}
          strokeWidth="3"
          stroke={color}
          fill="none"
          strokeLinecap="round"
          strokeDasharray={`${dash} ${circ - dash}`}
          className="transition-[stroke-dasharray] duration-700"
        />
      </svg>
      <span className="absolute inset-0 flex items-center justify-center text-[10px] font-bold text-white">{pct}%</span>
    </div>
  );
}
