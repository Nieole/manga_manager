import { Link } from 'react-router-dom';
import { ImageIcon } from 'lucide-react';
import type { Series } from './types';

interface RecentSeriesStripProps {
  recentSeries: Series[];
}

export function RecentSeriesStrip({ recentSeries }: RecentSeriesStripProps) {
  if (recentSeries.length === 0) {
    return null;
  }

  return (
    <div className="mb-10">
      <h3 className="text-xl font-bold text-white mb-4 pl-1 border-l-4 border-komgaPrimary">继续阅读</h3>
      <div className="flex gap-3 sm:gap-4 overflow-x-auto pb-4 custom-scrollbar snap-x">
        {recentSeries.map((series) => (
          <Link
            key={series.id}
            to={series.recent_book_id ? `/reader/${series.recent_book_id}` : `/series/${series.id}`}
            className="group shrink-0 w-32 sm:w-44 md:w-52 flex flex-col rounded-xl overflow-hidden bg-komgaSurface border border-gray-800 hover:border-komgaPrimary transition-all duration-300 hover:shadow-lg snap-start"
          >
            <div className="aspect-[2/3] w-full bg-gray-900 flex items-center justify-center relative overflow-hidden">
              {series.cover_path?.Valid && series.cover_path?.String ? (
                <img
                  src={`/api/thumbnails/${series.cover_path.String}${series.updated_at ? `?v=${new Date(series.updated_at).getTime()}` : ''}`}
                  alt={series.name}
                  className="w-full h-full object-cover transition-transform duration-500 group-hover:scale-105"
                  loading="lazy"
                />
              ) : (
                <ImageIcon className="w-10 h-10 text-gray-700" />
              )}
              <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-komgaBackground to-transparent h-2/3 opacity-80" />
              <div className="absolute inset-0 ring-1 ring-inset ring-white/10 rounded-t-xl" />
              <div className="absolute bottom-3 right-3 bg-komgaPrimary/90 text-white text-[10px] font-bold px-2 py-1 rounded backdrop-blur">
                接着读
              </div>
              {series.last_read_page?.Valid && series.last_read_page?.Int64 > 1 && (
                <div className="absolute top-2 right-2 bg-black/70 px-2 py-0.5 rounded text-[10px] text-gray-300 backdrop-blur">
                  P.{series.last_read_page.Int64}
                </div>
              )}
            </div>
            <div className="p-3">
              <h3
                className="text-sm font-semibold text-gray-100 truncate group-hover:text-komgaPrimary transition-colors"
                title={series.title?.Valid ? series.title.String : series.name}
              >
                {series.title?.Valid ? series.title.String : series.name}
              </h3>
            </div>
          </Link>
        ))}
      </div>
    </div>
  );
}
