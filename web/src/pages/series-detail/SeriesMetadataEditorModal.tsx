import { Lock, Unlock, X } from 'lucide-react';
import { useState } from 'react';
import type { Author, MetaTag, Series } from './types';

interface SeriesMetadataEditorModalProps {
  open: boolean;
  allTags: MetaTag[];
  allAuthors: Author[];
  editForm: Partial<Series> & {
    tagsInput?: string[];
    authorsInput?: { name: string; role: string }[];
    linksInput?: { name: string; url: string }[];
  };
  lockedFields: Set<string>;
  onClose: () => void;
  onSave: () => void;
  onToggleLock: (field: string) => void;
  onFormChange: (field: string, value: any) => void;
}

export function SeriesMetadataEditorModal({
  open,
  allTags,
  allAuthors,
  editForm,
  lockedFields,
  onClose,
  onSave,
  onToggleLock,
  onFormChange,
}: SeriesMetadataEditorModalProps) {
  const [tagInputValue, setTagInputValue] = useState('');
  const [authorInputName, setAuthorInputName] = useState('');
  const [authorInputRole, setAuthorInputRole] = useState('Writer');

  if (!open) return null;

  const currentTags = editForm.tagsInput || [];
  const tagSuggestions = allTags.filter(
    (tag) => !currentTags.includes(tag.name) && tag.name.toLowerCase().includes(tagInputValue.toLowerCase()),
  );

  const addTag = (name: string) => {
    if (name.trim() && !currentTags.includes(name.trim())) {
      onFormChange('tagsInput', [...currentTags, name.trim()]);
    }
    setTagInputValue('');
  };

  const removeTag = (name: string) => {
    onFormChange(
      'tagsInput',
      currentTags.filter((tag) => tag !== name),
    );
  };

  const currentAuthors = editForm.authorsInput || [];
  const authorSuggestions = allAuthors.filter(
    (author) =>
      !currentAuthors.find((item) => item.name === author.name && item.role === author.role) &&
      author.name.toLowerCase().includes(authorInputName.toLowerCase()),
  );

  const addAuthor = (name: string, role: string) => {
    if (name.trim() && !currentAuthors.find((item) => item.name === name.trim() && item.role === role)) {
      onFormChange('authorsInput', [...currentAuthors, { name: name.trim(), role }]);
    }
    setAuthorInputName('');
  };

  const removeAuthor = (idx: number) => {
    onFormChange(
      'authorsInput',
      currentAuthors.filter((_, index) => index !== idx),
    );
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm">
      <div className="bg-komgaSurface border border-gray-800 rounded-2xl w-full max-w-2xl overflow-hidden shadow-2xl flex flex-col max-h-[90vh]">
        <div className="flex items-center justify-between p-6 border-b border-gray-800 bg-gray-900/50">
          <h3 className="text-xl font-bold text-white">编辑系列元数据</h3>
          <button onClick={onClose} className="text-gray-400 hover:text-white transition-colors">
            <X className="w-6 h-6" />
          </button>
        </div>
        <div className="p-6 overflow-y-auto space-y-6 flex-1">
          {[
            { id: 'title', label: '系列标题 (Title)', type: 'text', val: editForm.title?.String || '' },
            { id: 'summary', label: '简介 (Summary)', type: 'textarea', val: editForm.summary?.String || '' },
            { id: 'publisher', label: '出版商 (Publisher)', type: 'text', val: editForm.publisher?.String || '' },
            { id: 'status', label: '连载状态 (Status)', type: 'select', val: editForm.status?.String || '', options: ['已完结', '连载中', '已放弃', '有生之年'] },
            { id: 'language', label: '语言 (Language ISO)', type: 'text', val: editForm.language?.String || '' },
            { id: 'rating', label: '评分 (Rating 0-10)', type: 'number', val: editForm.rating?.Float64 || 0, step: '0.1', max: 10 },
          ].map((field) => (
            <div key={field.id} className="space-y-2">
              <div className="flex items-center justify-between">
                <label className="text-sm font-medium text-gray-300">{field.label}</label>
                <button
                  onClick={() => onToggleLock(field.id)}
                  className={`flex items-center text-xs px-2 py-1 rounded transition-colors ${lockedFields.has(field.id) ? 'bg-orange-500/20 text-orange-400 border border-orange-500/30' : 'text-gray-500 hover:text-gray-300'}`}
                  title={lockedFields.has(field.id) ? '该字段已被锁定，扫描时不会被自动覆盖' : '点击锁定该字段，防止被扫描器覆盖'}
                >
                  {lockedFields.has(field.id) ? (
                    <>
                      <Lock className="w-3 h-3 mr-1" /> 已锁定防覆盖
                    </>
                  ) : (
                    <>
                      <Unlock className="w-3 h-3 mr-1" /> 未锁定
                    </>
                  )}
                </button>
              </div>
              {field.type === 'textarea' ? (
                <textarea
                  value={field.val}
                  onChange={(e) => onFormChange(field.id, e.target.value)}
                  className="w-full bg-gray-900 border border-gray-700 rounded-lg p-3 text-sm text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary/50 transition-all min-h-[100px]"
                />
              ) : field.type === 'select' ? (
                <select
                  value={field.val}
                  onChange={(e) => onFormChange(field.id, e.target.value)}
                  className="w-full bg-gray-900 border border-gray-700 rounded-lg p-3 text-sm text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary/50 transition-all cursor-pointer"
                >
                  <option value="">- 无状态 -</option>
                  {field.options?.map((option) => (
                    <option key={option} value={option}>
                      {option}
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  type={field.type}
                  step={field.step}
                  max={field.max}
                  value={field.val}
                  onChange={(e) => onFormChange(field.id, e.target.value)}
                  className="w-full bg-gray-900 border border-gray-700 rounded-lg p-3 text-sm text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary/50 transition-all"
                />
              )}
            </div>
          ))}

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <label className="text-sm font-medium text-gray-300">标签 (Tags)</label>
              <button
                onClick={() => onToggleLock('tags')}
                className={`flex items-center text-xs px-2 py-1 rounded transition-colors ${lockedFields.has('tags') ? 'bg-orange-500/20 text-orange-400 border border-orange-500/30' : 'text-gray-500 hover:text-gray-300'}`}
                title={lockedFields.has('tags') ? '已锁定该字段防覆盖' : '点击锁定防覆盖'}
              >
                {lockedFields.has('tags') ? (
                  <>
                    <Lock className="w-3 h-3 mr-1" /> 已锁定防覆盖
                  </>
                ) : (
                  <>
                    <Unlock className="w-3 h-3 mr-1" /> 未锁定
                  </>
                )}
              </button>
            </div>
            <div className="w-full bg-gray-900 border border-gray-700 rounded-lg p-2 text-sm text-white focus-within:ring-2 focus-within:ring-komgaPrimary/50 transition-all">
              <div className="flex flex-wrap gap-2 mb-2">
                {currentTags.map((tag) => (
                  <span key={tag} className="flex items-center gap-1 bg-komgaPrimary/20 text-komgaPrimary px-2 py-1 rounded text-xs border border-komgaPrimary/30">
                    {tag}
                    <button onClick={() => removeTag(tag)} className="hover:text-red-400">
                      <X className="w-3 h-3" />
                    </button>
                  </span>
                ))}
              </div>
              <div className="relative">
                <input
                  type="text"
                  value={tagInputValue}
                  onChange={(e) => setTagInputValue(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      addTag(tagInputValue);
                    }
                  }}
                  placeholder="输入标签按回车添加..."
                  className="w-full bg-transparent border-none outline-none p-1 text-sm placeholder-gray-500"
                />
                {tagInputValue && tagSuggestions.length > 0 && (
                  <div className="absolute top-10 left-0 w-full bg-komgaSurface border border-gray-700 rounded-lg shadow-xl z-20 max-h-40 overflow-y-auto">
                    {tagSuggestions.map((suggestion) => (
                      <div
                        key={suggestion.id}
                        onClick={() => addTag(suggestion.name)}
                        className="px-3 py-2 hover:bg-gray-800 cursor-pointer text-gray-300"
                      >
                        {suggestion.name}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <label className="text-sm font-medium text-gray-300">编绘者 (Authors)</label>
              <button
                onClick={() => onToggleLock('authors')}
                className={`flex items-center text-xs px-2 py-1 rounded transition-colors ${lockedFields.has('authors') ? 'bg-orange-500/20 text-orange-400 border border-orange-500/30' : 'text-gray-500 hover:text-gray-300'}`}
                title={lockedFields.has('authors') ? '已锁定该字段防覆盖' : '点击锁定防覆盖'}
              >
                {lockedFields.has('authors') ? (
                  <>
                    <Lock className="w-3 h-3 mr-1" /> 已锁定防覆盖
                  </>
                ) : (
                  <>
                    <Unlock className="w-3 h-3 mr-1" /> 未锁定
                  </>
                )}
              </button>
            </div>
            <div className="w-full bg-gray-900 border border-gray-700 rounded-lg p-2 text-sm text-white focus-within:ring-2 focus-within:ring-komgaPrimary/50 transition-all">
              <div className="flex flex-wrap gap-2 mb-2">
                {currentAuthors.map((author, idx) => (
                  <span key={idx} className="flex items-center gap-1 bg-gray-800 text-gray-300 px-2 py-1 rounded text-xs border border-gray-700">
                    {author.name} <span className="text-gray-500">[{author.role}]</span>
                    <button onClick={() => removeAuthor(idx)} className="hover:text-red-400 ml-1">
                      <X className="w-3 h-3" />
                    </button>
                  </span>
                ))}
              </div>
              <div className="flex gap-2 relative">
                <input
                  type="text"
                  value={authorInputName}
                  onChange={(e) => setAuthorInputName(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      addAuthor(authorInputName, authorInputRole);
                    }
                  }}
                  placeholder="输入作者并按回车..."
                  className="flex-1 bg-transparent border border-gray-800 rounded px-2 py-1 outline-none text-sm placeholder-gray-500"
                />
                <select
                  value={authorInputRole}
                  onChange={(e) => setAuthorInputRole(e.target.value)}
                  className="bg-gray-800 border-none outline-none rounded px-2 py-1 text-sm text-gray-300 cursor-pointer"
                >
                  <option value="Writer">Writer</option>
                  <option value="Penciller">Penciller</option>
                  <option value="Inker">Inker</option>
                  <option value="Colorist">Colorist</option>
                  <option value="Letterer">Letterer</option>
                  <option value="Cover">Cover</option>
                  <option value="Editor">Editor</option>
                </select>
                {authorInputName && authorSuggestions.length > 0 && (
                  <div className="absolute top-10 left-0 w-full bg-komgaSurface border border-gray-700 rounded-lg shadow-xl z-20 max-h-40 overflow-y-auto">
                    {authorSuggestions.map((suggestion) => (
                      <div
                        key={suggestion.id + suggestion.role}
                        onClick={() => addAuthor(suggestion.name, suggestion.role)}
                        className="px-3 py-2 hover:bg-gray-800 cursor-pointer flex justify-between text-gray-300"
                      >
                        <span>{suggestion.name}</span>
                        <span className="text-gray-500 text-xs mt-0.5">{suggestion.role}</span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          </div>

          <div className="space-y-2">
            <label className="text-sm font-medium text-gray-300">外部链接 (External Links)</label>
            <div className="space-y-3">
              {(editForm.linksInput || []).map((link, idx) => (
                <div key={idx} className="flex gap-2 items-center">
                  <input
                    type="text"
                    value={link.name}
                    onChange={(e) => {
                      const newLinks = [...(editForm.linksInput || [])];
                      newLinks[idx].name = e.target.value;
                      onFormChange('linksInput', newLinks);
                    }}
                    placeholder="Link Name (e.g. Anilist)"
                    className="flex-1 bg-gray-900 border border-gray-700 rounded p-2 text-sm text-white focus:outline-none focus:ring-1 focus:ring-komgaPrimary"
                  />
                  <input
                    type="text"
                    value={link.url}
                    onChange={(e) => {
                      const newLinks = [...(editForm.linksInput || [])];
                      newLinks[idx].url = e.target.value;
                      onFormChange('linksInput', newLinks);
                    }}
                    placeholder="URL"
                    className="flex-[2] bg-gray-900 border border-gray-700 rounded p-2 text-sm text-white focus:outline-none focus:ring-1 focus:ring-komgaPrimary"
                  />
                  <button
                    onClick={() => {
                      const newLinks = (editForm.linksInput || []).filter((_, index) => index !== idx);
                      onFormChange('linksInput', newLinks);
                    }}
                    className="p-2 text-red-400 hover:bg-gray-800 rounded"
                  >
                    <X className="w-4 h-4" />
                  </button>
                </div>
              ))}
              <button
                onClick={() => {
                  const newLinks = [...(editForm.linksInput || []), { name: '', url: '' }];
                  onFormChange('linksInput', newLinks);
                }}
                className="text-xs text-komgaPrimary font-medium border border-komgaPrimary/30 bg-komgaPrimary/10 hover:bg-komgaPrimary/20 px-3 py-1.5 rounded transition-colors block w-full text-center"
              >
                + 添加外部链接
              </button>
            </div>
          </div>
        </div>
        <div className="p-6 border-t border-gray-800 bg-gray-900/50 flex justify-end gap-3">
          <button
            onClick={onClose}
            className="px-5 py-2 rounded-lg text-sm font-medium text-gray-300 hover:bg-gray-800 transition-colors"
          >
            取消
          </button>
          <button
            onClick={onSave}
            className="px-5 py-2 rounded-lg text-sm font-medium bg-komgaPrimary text-white hover:bg-komgaPrimary/80 transition-colors shadow-lg shadow-komgaPrimary/20"
          >
            保存更改
          </button>
        </div>
      </div>
    </div>
  );
}
