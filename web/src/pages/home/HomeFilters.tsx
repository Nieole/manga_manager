import type { NamedOption } from './types';

interface HomeFiltersProps {
  allStatuses: string[];
  allTags: NamedOption[];
  allAuthors: NamedOption[];
  activeStatus: string | null;
  activeTag: string | null;
  activeAuthor: string | null;
  activeLetter: string | null;
  onStatusChange: (value: string | null) => void;
  onTagChange: (value: string | null) => void;
  onAuthorChange: (value: string | null) => void;
  onLetterChange: (value: string | null) => void;
}

export function HomeFilters({
  allStatuses,
  allTags,
  allAuthors,
  activeStatus,
  activeTag,
  activeAuthor,
  activeLetter,
  onStatusChange,
  onTagChange,
  onAuthorChange,
  onLetterChange,
}: HomeFiltersProps) {
  return (
    <>
      <div className="mb-6 grid xl:grid-cols-3 gap-6 xl:gap-8 divide-y xl:divide-y-0 xl:divide-x divide-gray-800">
        <div className="xl:pr-8">
          <span className="text-komgaPrimary font-semibold text-sm mb-3 block">连载状态 (Status)</span>
          <div className="flex flex-wrap gap-2 max-h-32 overflow-y-auto pr-2 custom-scrollbar">
            <button
              onClick={() => onStatusChange(null)}
              className={`px-3 py-1 text-xs font-medium rounded-full transition-colors border ${activeStatus === null ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20' : 'bg-transparent border-gray-700 text-gray-400 hover:border-gray-500 hover:text-white'}`}
            >
              全部状态
            </button>
            {allStatuses.map((status) => (
              <button
                key={status}
                onClick={() => onStatusChange(activeStatus === status ? null : status)}
                className={`px-3 py-1 text-xs font-medium rounded-full transition-all border flex items-center gap-1.5 ${activeStatus === status ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20' : 'bg-transparent border-gray-800 text-gray-400 hover:border-gray-600 hover:text-gray-200 bg-gray-900/40'}`}
              >
                {status}
              </button>
            ))}
          </div>
        </div>

        {allTags.length > 0 && (
          <div className="pt-6 xl:pt-0 xl:px-8">
            <span className="text-komgaPrimary font-semibold text-sm mb-3 block">标签分类 (Tags)</span>
            <div className="flex flex-wrap gap-2 max-h-32 overflow-y-auto pr-2 custom-scrollbar">
              <button
                onClick={() => onTagChange(null)}
                className={`px-3 py-1 text-xs font-medium rounded-full transition-colors border ${activeTag === null ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20' : 'bg-transparent border-gray-700 text-gray-400 hover:border-gray-500 hover:text-white'}`}
              >
                不限
              </button>
              {allTags.map((tag) => (
                <button
                  key={tag.name}
                  onClick={() => onTagChange(tag.name === activeTag ? null : tag.name)}
                  className={`px-3 py-1 text-xs font-medium rounded-full transition-all border flex items-center gap-1.5 ${activeTag === tag.name ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20' : 'bg-transparent border-gray-800 text-gray-400 hover:border-gray-600 hover:text-gray-200 bg-gray-900/40'}`}
                >
                  {tag.name}
                </button>
              ))}
            </div>
          </div>
        )}

        {allAuthors.length > 0 && (
          <div className="pt-6 xl:pt-0 xl:pl-8">
            <span className="text-komgaPrimary font-semibold text-sm mb-3 block">参与人员 (Authors)</span>
            <div className="flex flex-wrap gap-2 max-h-32 overflow-y-auto pr-2 custom-scrollbar">
              <button
                onClick={() => onAuthorChange(null)}
                className={`px-3 py-1 text-xs font-medium rounded-full transition-colors border ${activeAuthor === null ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20' : 'bg-transparent border-gray-700 text-gray-400 hover:border-gray-500 hover:text-white'}`}
              >
                不限
              </button>
              {allAuthors.map((author) => (
                <button
                  key={author.name}
                  onClick={() => onAuthorChange(activeAuthor === author.name ? null : author.name)}
                  className={`px-3 py-1 text-xs font-medium rounded-full transition-all border flex items-center gap-1.5 ${activeAuthor === author.name ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-lg shadow-komgaPrimary/20' : 'bg-transparent border-gray-800 text-gray-400 hover:border-gray-600 hover:text-gray-200 bg-gray-900/40'}`}
                >
                  {author.name}
                </button>
              ))}
            </div>
          </div>
        )}
      </div>

      <div className="mb-8 flex flex-wrap gap-1 items-center justify-center">
        <button
          onClick={() => onLetterChange(null)}
          className={`px-3 py-1.5 text-sm font-medium rounded-md transition-colors ${activeLetter === null ? 'bg-komgaPrimary text-white shadow-md' : 'text-gray-400 hover:bg-gray-800 hover:text-white'}`}
        >
          全部
        </button>
        {'ABCDEFGHIJKLMNOPQRSTUVWXYZ#'.split('').map((letter) => (
          <button
            key={letter}
            onClick={() => onLetterChange(activeLetter === letter ? null : letter)}
            className={`w-8 h-8 flex items-center justify-center text-sm font-medium rounded-md transition-colors ${activeLetter === letter ? 'bg-komgaPrimary text-white shadow-md' : 'text-gray-400 hover:bg-gray-800 hover:text-white'}`}
          >
            {letter}
          </button>
        ))}
      </div>
    </>
  );
}
