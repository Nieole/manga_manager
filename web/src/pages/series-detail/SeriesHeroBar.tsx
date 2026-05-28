import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import {
  ArrowLeft,
  ArrowRight,
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
      <div className="flex flex-wrap items-center justify-between gap-2 mb-6">
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

        <div className="relative grid grid-cols-1 sm:grid-cols-[auto_1fr] gap-6 lg:gap-8 items-start">
          {/* 封面块 */}
          <div className="relative mx-auto sm:mx-0">
            <div className="relative w-32 sm:w-44 lg:w-52">
              <div className="absolute inset-0 rounded-2xl bg-gradient-to-br from-komgaPrimary/40 to-komgaSecondary/40 blur-xl opacity-60" />
              <div className="relative rounded-2xl overflow-hidden border border-white/10 bg-komgaSurface shadow-2xl shadow-black/40 ring-1 ring-white/5">
                {coverUrl ? (
                  <img src={coverUrl} alt={t('common.cover')} className="w-full h-auto object-cover aspect-[2/3]" />
                ) : (
                  <div className="w-full aspect-[2/3] flex items-center justify-center bg-komgaSurface/50">
                    <BookImage className="w-12 h-12 text-gray-500 opacity-50" />
                  </div>
                )}
              </div>
            </div>
          </div>

          {/* 信息块 */}
          <div className="min-w-0 w-full">
            {/* 状态 + 作者前置行 */}
            <div className="flex flex-wrap items-center gap-x-3 gap-y-2 mb-3 justify-center sm:justify-start">
              {statusLabel && (
                <span className="inline-flex items-center gap-1.5 text-xs font-medium text-gray-200">
                  <span className={`relative flex h-2 w-2 rounded-full ${statusDotColor(status)} shadow-[0_0_10px]`}>
                    <span className={`absolute inset-0 rounded-full ${statusDotColor(status).split(' ')[0]} animate-ping opacity-60`} />
                  </span>
                  <span className="uppercase tracking-wider">{statusLabel}</span>
                </span>
              )}
              {authors.length > 0 && (
                <span className="inline-flex items-center gap-1.5 text-xs text-gray-400">
                  <span className="text-gray-500">{t('series.header.byAuthors')}</span>
                  {authors.slice(0, 3).map((a, i) => (
                    <button
                      key={a.id}
                      type="button"
                      onClick={() => navigate(libraryFilterTo(series?.library_id, 'author', a.name))}
                      className="font-semibold text-gray-100 hover:text-komgaPrimary transition-colors"
                    >
                      {a.name}
                      {i < Math.min(authors.length, 3) - 1 ? <span className="text-gray-600 mx-1">·</span> : null}
                    </button>
                  ))}
                  {authors.length > 3 && <span className="text-gray-500">+{authors.length - 3}</span>}
                </span>
              )}
            </div>

            {/* 大标题 */}
            <h1 className="font-black text-white tracking-tight break-words leading-[1.05] mb-4 text-center sm:text-left text-3xl sm:text-4xl lg:text-5xl xl:text-6xl">
              <span className="bg-gradient-to-br from-white via-white to-white/70 bg-clip-text text-transparent">
                {title}
              </span>
            </h1>

            {/* 简介 */}
            {series?.summary?.Valid && series.summary.String && (
              <p className="text-sm sm:text-base text-gray-300/90 leading-relaxed line-clamp-3 hover:line-clamp-none transition-all cursor-pointer max-w-3xl mb-5 text-center sm:text-left">
                {series.summary.String}
              </p>
            )}

            {/* 主操作 + 进度 行 */}
            <div className="flex flex-col sm:flex-row gap-3 mb-5">
              {continueCta && (
                <Link
                  to={`/reader/${continueCta.bookId}`}
                  className="group relative inline-flex items-center justify-center sm:justify-start gap-3 px-5 py-3 rounded-2xl bg-gradient-to-r from-komgaPrimary to-komgaPrimaryHover text-white font-semibold shadow-xl shadow-komgaPrimary/40 hover:shadow-2xl hover:shadow-komgaPrimary/60 hover:-translate-y-0.5 transition-all overflow-hidden"
                >
                  <span className="absolute inset-0 bg-gradient-to-r from-white/0 via-white/20 to-white/0 -translate-x-full group-hover:translate-x-full transition-transform duration-700" />
                  <span className="relative flex h-9 w-9 items-center justify-center rounded-full bg-white/20 backdrop-blur shrink-0">
                    <Play className="w-4 h-4 fill-current translate-x-px" />
                  </span>
                  <span className="relative text-sm sm:text-base">{ctaLabel}</span>
                  <ArrowRight className="relative w-4 h-4 opacity-70 group-hover:translate-x-1 transition-transform shrink-0" />
                </Link>
              )}
              {totalPages > 0 && (
                <div className="flex items-center gap-3 px-4 py-2.5 rounded-2xl bg-white/5 border border-white/10 backdrop-blur-md">
                  <ProgressRing pct={progressPct} finished={!!hasFinished} />
                  <div className="flex flex-col leading-tight">
                    <span className="text-xs text-gray-400 uppercase tracking-wider">
                      {hasFinished ? t('series.stats.completed') : t('series.stats.progress')}
                    </span>
                    <span className="text-sm font-semibold text-white">
                      {t('series.stats.pages', { read: readPages.toLocaleString(), total: totalPages.toLocaleString() })}
                    </span>
                  </div>
                </div>
              )}
            </div>

            {/* 统计卡片组：评分 / 册数 / 卷数 / 单行本 / 语言 / 出版社 */}
            <div className="flex flex-wrap gap-2 mb-4 justify-center sm:justify-start">
              {rating > 0 && (
                <StatCard
                  tone="amber"
                  icon={<Star className="w-3.5 h-3.5 fill-current" />}
                  label={t('series.stats.rating')}
                  value={rating.toFixed(1)}
                />
              )}
              <StatCard
                tone="indigo"
                icon={<BookOpen className="w-3.5 h-3.5" />}
                label={t('series.stats.books', { count: bookCount })}
              />
              {volumeCount > 0 && (
                <StatCard
                  tone="violet"
                  icon={<Sparkles className="w-3.5 h-3.5" />}
                  label={t('series.stats.volumes', { count: volumeCount })}
                />
              )}
              {standaloneCount > 0 && (
                <StatCard
                  tone="cyan"
                  icon={<BookImage className="w-3.5 h-3.5" />}
                  label={t('series.stats.standalones', { count: standaloneCount })}
                />
              )}
              {language && (
                <StatCard
                  tone="emerald"
                  icon={<Globe className="w-3.5 h-3.5" />}
                  label={language.toUpperCase()}
                />
              )}
              {publisher && (
                <StatCard
                  tone="rose"
                  icon={<Building2 className="w-3.5 h-3.5" />}
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

type StatTone = 'amber' | 'indigo' | 'violet' | 'cyan' | 'emerald' | 'rose';

const TONE_STYLES: Record<StatTone, string> = {
  amber: 'bg-amber-400/10 text-amber-300 border-amber-400/30 hover:bg-amber-400/15',
  indigo: 'bg-indigo-400/10 text-indigo-300 border-indigo-400/30 hover:bg-indigo-400/15',
  violet: 'bg-violet-400/10 text-violet-300 border-violet-400/30 hover:bg-violet-400/15',
  cyan: 'bg-cyan-400/10 text-cyan-300 border-cyan-400/30 hover:bg-cyan-400/15',
  emerald: 'bg-emerald-400/10 text-emerald-300 border-emerald-400/30 hover:bg-emerald-400/15',
  rose: 'bg-rose-400/10 text-rose-300 border-rose-400/30 hover:bg-rose-400/15',
};

function StatCard({
  tone,
  icon,
  label,
  value,
}: {
  tone: StatTone;
  icon: React.ReactNode;
  label: string;
  value?: string;
}) {
  return (
    <div
      className={`inline-flex items-center gap-2 px-3 py-1.5 rounded-xl border backdrop-blur-sm transition-colors ${TONE_STYLES[tone]}`}
      title={value ? `${label}: ${value}` : label}
    >
      <span className="shrink-0">{icon}</span>
      {value ? (
        <span className="flex items-baseline gap-1.5 leading-none">
          <span className="text-sm font-bold tabular-nums">{value}</span>
          <span className="text-[10px] uppercase tracking-wider opacity-70">{label}</span>
        </span>
      ) : (
        <span className="text-xs font-medium leading-none truncate max-w-[14rem]">{label}</span>
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
