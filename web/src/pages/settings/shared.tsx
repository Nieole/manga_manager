import type { ReactNode } from 'react';
import { Save } from 'lucide-react';

export const sectionClassName = 'bg-komgaSurface border border-gray-800 rounded-2xl p-6 shadow-sm space-y-4';
export const inputClassName = 'w-full bg-gray-900 border border-gray-800 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary/40 transition-all';

export function FieldErrors({ messages }: { messages: string[] }) {
  if (!messages.length) return null;
  return (
    <>
      {messages.map((message) => (
        <p key={message} className="mt-1 text-xs text-red-300">
          {message}
        </p>
      ))}
    </>
  );
}

export function SettingsPageIntro({
  title,
  description,
  badge,
}: {
  title: string;
  description: string;
  badge?: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
      <div>
        <h2 className="text-2xl font-bold tracking-tight text-white">{title}</h2>
        <p className="mt-2 text-sm leading-6 text-gray-400">{description}</p>
      </div>
      {badge}
    </div>
  );
}

export function SettingsSaveBar({
  saving,
  label,
  hint,
  onSave,
}: {
  saving: boolean;
  label: string;
  hint?: string;
  onSave: () => void;
}) {
  return (
    <div className="sticky bottom-0 z-10 -mx-4 mt-6 border-t border-gray-800 bg-komgaDark/90 px-4 py-4 backdrop-blur-md sm:-mx-8">
      <div className="mx-auto flex max-w-5xl items-center justify-between gap-4">
        <div className="text-sm text-gray-500">{hint || '修改只作用于当前设置分组。'}</div>
        <button
          onClick={onSave}
          disabled={saving}
          className="inline-flex items-center gap-2 rounded-xl bg-komgaPrimary px-5 py-3 text-sm font-medium text-white shadow-lg hover:bg-komgaPrimaryHover disabled:opacity-60"
        >
          <Save className={`h-4 w-4 ${saving ? 'animate-spin' : ''}`} />
          {saving ? '保存中...' : label}
        </button>
      </div>
    </div>
  );
}
