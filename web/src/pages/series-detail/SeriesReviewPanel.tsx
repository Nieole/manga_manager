/**
 * 业务说明：本文件是「个人系列短评」面板（第 6 项），嵌在系列详情侧栏的「短评」页。
 * 当前用户可给系列打分（1-5 星）并写一段短评，保存到 /api/series/{id}/review（每用户私有，与全局刮削评分区分）。
 * 维护要点：自包含 GET/PUT/DELETE；评分与短评都清空即删除该条；未登录时后端返回空、保存为无操作。
 */

import { useEffect, useState } from 'react';
import { Loader2, Star } from 'lucide-react';
import { apiClient, getApiErrorMessage } from '../../api/client';
import { useI18n } from '../../i18n/LocaleProvider';
import { useToast } from '../../components/ToastProvider';

interface ReviewResponse {
  exists: boolean;
  rating: number | null;
  review: string;
}

export function SeriesReviewPanel({ seriesId }: { seriesId: number }) {
  const { t } = useI18n();
  const { showToast } = useToast();
  const [rating, setRating] = useState<number | null>(null);
  const [review, setReview] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    // 切换系列时先清空——否则若新系列的 GET 失败，面板仍残留上一个系列的评分/短评，
    // 用户点保存会把旧内容写到新系列（跨系列串写）。
    setRating(null);
    setReview('');
    apiClient.get<ReviewResponse>(`/api/series/${seriesId}/review`)
      .then((r) => {
        if (cancelled) return;
        setRating(r.data.rating ?? null);
        setReview(r.data.review ?? '');
      })
      .catch((err) => { if (!cancelled) showToast(getApiErrorMessage(err, t('seriesReview.loadFailed')), 'error'); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [seriesId, t, showToast]);

  const save = async () => {
    setSaving(true);
    try {
      await apiClient.put(`/api/series/${seriesId}/review`, { rating, review: review.trim() });
      showToast(t('seriesReview.saved'), 'success');
    } catch (err) {
      showToast(getApiErrorMessage(err, t('seriesReview.saveFailed')), 'error');
    } finally {
      setSaving(false);
    }
  };

  const clear = () => {
    setRating(null);
    setReview('');
  };

  if (loading) {
    return <div className="flex justify-center py-10"><Loader2 className="h-5 w-5 animate-spin text-komgaPrimary" /></div>;
  }

  return (
    <div className="space-y-5">
      <div>
        <div className="text-sm font-medium text-gray-300 mb-2">{t('seriesReview.ratingLabel')}</div>
        <div className="flex items-center gap-1">
          {[1, 2, 3, 4, 5].map((n) => (
            <button
              key={n}
              type="button"
              onClick={() => setRating(rating === n ? null : n)}
              className="p-1 text-gray-500 hover:text-amber-400 transition-colors"
              aria-label={`${n}`}
            >
              <Star className={`h-6 w-6 ${rating !== null && n <= rating ? 'fill-amber-400 text-amber-400' : ''}`} />
            </button>
          ))}
          {rating !== null && <span className="ml-2 text-sm text-gray-400">{rating}/5</span>}
        </div>
      </div>

      <div>
        <div className="text-sm font-medium text-gray-300 mb-2">{t('seriesReview.heading')}</div>
        <textarea
          value={review}
          onChange={(e) => setReview(e.target.value)}
          placeholder={t('seriesReview.notePlaceholder')}
          rows={6}
          className="w-full resize-y rounded-lg border border-gray-800 bg-gray-900 px-3 py-2.5 text-sm text-white placeholder:text-gray-500 focus:outline-hidden focus:ring-2 focus:ring-komgaPrimary/40"
        />
      </div>

      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={save}
          disabled={saving}
          className="inline-flex items-center gap-2 rounded-lg bg-komgaPrimary hover:bg-komgaPrimaryHover disabled:opacity-50 px-4 py-2 text-sm font-medium text-white transition-colors"
        >
          {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
          {saving ? t('seriesReview.saving') : t('seriesReview.save')}
        </button>
        <button
          type="button"
          onClick={clear}
          disabled={saving}
          className="rounded-lg border border-gray-800 px-4 py-2 text-sm text-gray-300 hover:bg-gray-800 hover:text-white transition-colors"
        >
          {t('seriesReview.clear')}
        </button>
      </div>
    </div>
  );
}
