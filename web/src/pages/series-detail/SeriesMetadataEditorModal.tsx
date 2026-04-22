import { Lock, Sparkles, Unlock, X } from 'lucide-react';
import { useState } from 'react';
import type { Author, MetaTag, Series } from './types';
import { ModalShell } from '../../components/ui/ModalShell';
import { useI18n } from '../../i18n/LocaleProvider';
import { normalizeSeriesStatus } from '../../i18n/status';
import {
  modalGhostButtonClass,
  modalInputClass,
  modalPrimaryButtonClass,
  modalSectionClass,
  modalSelectClass,
  modalSubtleTagClass,
  modalTagClass,
  modalTextareaClass,
} from '../../components/ui/modalStyles';

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
  const { t } = useI18n();
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

  const statusOptions = ['completed', 'ongoing', 'cancelled', 'hiatus'] as const;

  return (
    <ModalShell
      open={open}
      onClose={onClose}
      title={t('series.editor.title')}
      description={t('series.editor.description')}
      icon={<Sparkles className="h-5 w-5" />}
      size="standard"
      footer={
        <div className="flex flex-col-reverse justify-end gap-3 sm:flex-row">
          <button onClick={onClose} className={modalGhostButtonClass}>
            {t('modal.cancel')}
          </button>
          <button onClick={onSave} className={modalPrimaryButtonClass}>
            {t('series.editor.save')}
          </button>
        </div>
      }
    >
        <div className="space-y-6">
          {[
            { id: 'title', label: t('series.editor.field.title'), type: 'text', val: editForm.title?.String || '' },
            { id: 'summary', label: t('series.editor.field.summary'), type: 'textarea', val: editForm.summary?.String || '' },
            { id: 'publisher', label: t('series.editor.field.publisher'), type: 'text', val: editForm.publisher?.String || '' },
            { id: 'status', label: t('series.editor.field.status'), type: 'select', val: normalizeSeriesStatus(editForm.status?.String), options: statusOptions },
            { id: 'language', label: t('series.editor.field.language'), type: 'text', val: editForm.language?.String || '' },
            { id: 'rating', label: t('series.editor.field.rating'), type: 'number', val: editForm.rating?.Float64 || 0, step: '0.1', max: 10 },
          ].map((field) => (
            <div key={field.id} className={`${modalSectionClass} space-y-3`}>
              <div className="flex items-center justify-between">
                <label className="text-sm font-medium text-gray-300">{field.label}</label>
                <button
                  onClick={() => onToggleLock(field.id)}
                  className={`flex items-center rounded-lg border px-2.5 py-1.5 text-xs transition-colors ${lockedFields.has(field.id) ? 'border-orange-500/30 bg-orange-500/20 text-orange-300' : 'border-gray-700 bg-gray-900/60 text-gray-400 hover:text-gray-200'}`}
                  title={lockedFields.has(field.id) ? t('series.editor.lockedTitle') : t('series.editor.unlockedTitle')}
                >
                  {lockedFields.has(field.id) ? (
                    <>
                      <Lock className="w-3 h-3 mr-1" /> {t('series.editor.locked')}
                    </>
                  ) : (
                    <>
                      <Unlock className="w-3 h-3 mr-1" /> {t('series.editor.unlocked')}
                    </>
                  )}
                </button>
              </div>
              {field.type === 'textarea' ? (
                <textarea
                  value={field.val}
                  onChange={(e) => onFormChange(field.id, e.target.value)}
                  className={modalTextareaClass}
                />
              ) : field.type === 'select' ? (
                <select
                  value={field.val}
                  onChange={(e) => onFormChange(field.id, e.target.value)}
                  className={modalSelectClass}
                >
                  <option value="unknown">{t('series.editor.noStatus')}</option>
                  {field.options?.map((option) => (
                    <option key={option} value={option}>
                      {t(`status.${option}`)}
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
                  className={modalInputClass}
                />
              )}
            </div>
          ))}

          <div className={`${modalSectionClass} space-y-3`}>
            <div className="flex items-center justify-between">
              <label className="text-sm font-medium text-gray-300">{t('series.editor.field.tags')}</label>
              <button
                onClick={() => onToggleLock('tags')}
                className={`flex items-center rounded-lg border px-2.5 py-1.5 text-xs transition-colors ${lockedFields.has('tags') ? 'border-orange-500/30 bg-orange-500/20 text-orange-300' : 'border-gray-700 bg-gray-900/60 text-gray-400 hover:text-gray-200'}`}
                title={lockedFields.has('tags') ? t('series.editor.lockedTitle') : t('series.editor.unlockedTitle')}
              >
                {lockedFields.has('tags') ? (
                  <>
                    <Lock className="w-3 h-3 mr-1" /> {t('series.editor.locked')}
                  </>
                ) : (
                  <>
                    <Unlock className="w-3 h-3 mr-1" /> {t('series.editor.unlocked')}
                  </>
                )}
              </button>
            </div>
            <div className="w-full rounded-2xl border border-gray-700 bg-gray-950/80 p-3 text-sm text-white shadow-inner shadow-black/20 transition-all focus-within:border-komgaPrimary/50 focus-within:ring-2 focus-within:ring-komgaPrimary/20">
              <div className="flex flex-wrap gap-2 mb-2">
                {currentTags.map((tag) => (
                  <span key={tag} className={modalTagClass}>
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
                  placeholder={t('series.editor.tagPlaceholder')}
                  className="w-full bg-transparent border-none p-1 text-sm outline-none placeholder-gray-500"
                />
                {tagInputValue && tagSuggestions.length > 0 && (
                  <div className="absolute left-0 top-10 z-20 max-h-40 w-full overflow-y-auto rounded-xl border border-gray-700 bg-komgaSurface shadow-xl">
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

          <div className={`${modalSectionClass} space-y-3`}>
            <div className="flex items-center justify-between">
              <label className="text-sm font-medium text-gray-300">{t('series.editor.field.authors')}</label>
              <button
                onClick={() => onToggleLock('authors')}
                className={`flex items-center rounded-lg border px-2.5 py-1.5 text-xs transition-colors ${lockedFields.has('authors') ? 'border-orange-500/30 bg-orange-500/20 text-orange-300' : 'border-gray-700 bg-gray-900/60 text-gray-400 hover:text-gray-200'}`}
                title={lockedFields.has('authors') ? t('series.editor.lockedTitle') : t('series.editor.unlockedTitle')}
              >
                {lockedFields.has('authors') ? (
                  <>
                    <Lock className="w-3 h-3 mr-1" /> {t('series.editor.locked')}
                  </>
                ) : (
                  <>
                    <Unlock className="w-3 h-3 mr-1" /> {t('series.editor.unlocked')}
                  </>
                )}
              </button>
            </div>
            <div className="w-full rounded-2xl border border-gray-700 bg-gray-950/80 p-3 text-sm text-white shadow-inner shadow-black/20 transition-all focus-within:border-komgaPrimary/50 focus-within:ring-2 focus-within:ring-komgaPrimary/20">
              <div className="flex flex-wrap gap-2 mb-2">
                {currentAuthors.map((author, idx) => (
                  <span key={idx} className={modalSubtleTagClass}>
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
                  placeholder={t('series.editor.authorPlaceholder')}
                  className="flex-1 rounded-lg border border-gray-800 bg-black/20 px-2.5 py-2 text-sm outline-none placeholder-gray-500"
                />
                <select
                  value={authorInputRole}
                  onChange={(e) => setAuthorInputRole(e.target.value)}
                  className="rounded-lg border border-gray-800 bg-gray-800 px-2.5 py-2 text-sm text-gray-300 outline-none cursor-pointer"
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
                  <div className="absolute left-0 top-10 z-20 max-h-40 w-full overflow-y-auto rounded-xl border border-gray-700 bg-komgaSurface shadow-xl">
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

          <div className={`${modalSectionClass} space-y-3`}>
            <label className="text-sm font-medium text-gray-300">{t('series.editor.field.links')}</label>
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
                    className="flex-1 rounded-xl border border-gray-700 bg-gray-950/80 px-3 py-2.5 text-sm text-white outline-none transition-all focus:border-komgaPrimary/50 focus:ring-2 focus:ring-komgaPrimary/20"
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
                    className="flex-[2] rounded-xl border border-gray-700 bg-gray-950/80 px-3 py-2.5 text-sm text-white outline-none transition-all focus:border-komgaPrimary/50 focus:ring-2 focus:ring-komgaPrimary/20"
                  />
                  <button
                    onClick={() => {
                      const newLinks = (editForm.linksInput || []).filter((_, index) => index !== idx);
                      onFormChange('linksInput', newLinks);
                    }}
                    className="rounded-xl border border-red-500/20 bg-red-500/10 p-2.5 text-red-300 transition-all hover:bg-red-500/20"
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
                className="block w-full rounded-xl border border-komgaPrimary/30 bg-komgaPrimary/10 px-3 py-2 text-center text-xs font-medium text-komgaPrimary transition-colors hover:bg-komgaPrimary/20"
              >
                {t('series.editor.addLink')}
              </button>
            </div>
          </div>
        </div>
    </ModalShell>
  );
}
