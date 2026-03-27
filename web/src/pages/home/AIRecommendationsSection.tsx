import { Link } from 'react-router-dom';
import { ImageIcon } from 'lucide-react';
import type { AIRecommendation } from './types';

interface AIRecommendationsSectionProps {
  aiRecommendations: AIRecommendation[];
  loadingAI: boolean;
  hasFetchedAI: boolean;
  onRefresh: () => void;
}

export function AIRecommendationsSection({
  aiRecommendations,
  loadingAI,
  hasFetchedAI,
  onRefresh,
}: AIRecommendationsSectionProps) {
  return (
    <div className="mb-10">
      <div className="flex items-center gap-3 mb-4 pl-1 border-l-4 border-komgaPrimary">
        <h3 className="text-xl font-bold text-white">AI 每日导读</h3>
        <button
          onClick={onRefresh}
          disabled={loadingAI}
          className="text-xs px-2 py-1 rounded bg-komgaPrimary/20 text-komgaPrimary hover:bg-komgaPrimary/30 transition-colors flex items-center gap-1 border border-komgaPrimary/30 disabled:opacity-50"
        >
          {loadingAI ? 'AI 思考中...' : hasFetchedAI ? '换一批' : '生成专属推荐'}
        </button>
      </div>

      {loadingAI ? (
        <div className="bg-gray-900/50 border border-gray-800 rounded-xl p-8 flex flex-col items-center justify-center">
          <div className="animate-pulse flex flex-col items-center">
            <div className="w-10 h-10 border-4 border-komgaPrimary border-t-transparent rounded-full animate-spin mb-4" />
            <p className="text-gray-400 text-sm">正在深度推演你的阅读偏好...</p>
          </div>
        </div>
      ) : aiRecommendations.length > 0 ? (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {aiRecommendations.map((rec, index) => (
            <Link
              key={`rec-${rec.series_id}-${index}`}
              to={`/series/${rec.series_id}`}
              className="group flex gap-4 bg-komgaSurface border border-gray-800 hover:border-komgaPrimary/50 overflow-hidden rounded-xl p-3 transition-colors hover:bg-gray-900/50 shadow-sm"
            >
              <div className="w-20 aspect-[2/3] shrink-0 bg-black rounded-lg overflow-hidden relative border border-gray-800">
                {rec.cover_path ? (
                  <img
                    src={`/api/thumbnails/${rec.cover_path}`}
                    alt={rec.title}
                    className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-300"
                  />
                ) : (
                  <div className="w-full h-full flex items-center justify-center text-gray-700">
                    <ImageIcon className="w-6 h-6" />
                  </div>
                )}
              </div>
              <div className="flex flex-col flex-1 py-1 overflow-hidden">
                <h4 className="font-bold text-gray-200 text-sm mb-1.5 line-clamp-1 group-hover:text-komgaPrimary transition-colors">
                  {rec.title}
                </h4>
                <div className="bg-black/40 rounded p-2.5 text-xs text-gray-400 italic flex-1 relative flex">
                  <span className="text-komgaPrimary font-serif text-2xl leading-none absolute -top-1.5 -left-0.5 opacity-40">"</span>
                  <span className="pl-4 leading-relaxed line-clamp-3 relative z-10">{rec.reason}</span>
                </div>
              </div>
            </Link>
          ))}
        </div>
      ) : hasFetchedAI ? (
        <div className="bg-gray-900/50 border border-gray-800 rounded-xl p-6 text-center text-gray-500 text-sm">
          暂时没有找到合适的推荐
        </div>
      ) : null}
    </div>
  );
}
