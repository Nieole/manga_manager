/**
 * 业务说明：本文件是应用外壳侧边栏的两个基础导航组件——可折叠分组标题（SidebarGroup）与
 * 带激活态高亮的导航链接（SidebarLink），供 Layout 的侧栏各分组复用。
 * 维护时应保持折叠态/展开态的视觉一致与激活匹配逻辑。
 */

import { Link } from 'react-router-dom';
import { ChevronDown } from 'lucide-react';
import type { ReactNode } from 'react';

interface SidebarGroupProps {
  label: string;
  collapsed: boolean;
  expanded: boolean;
  onToggle: () => void;
  collapsedIcon: ReactNode;
  children: ReactNode;
}

export function SidebarGroup({ label, collapsed, expanded, onToggle, collapsedIcon, children }: SidebarGroupProps) {
  return (
    <div className="space-y-1">
      {!collapsed ? (
        <button
          onClick={onToggle}
          className="w-full flex items-center justify-between px-3 py-1.5 text-[11px] font-semibold tracking-wider text-gray-500 uppercase rounded-md hover:bg-gray-800/40 hover:text-gray-300 transition-colors"
        >
          <span>{label}</span>
          <ChevronDown className={`w-3 h-3 transition-transform duration-200 ${expanded ? 'rotate-0' : '-rotate-90'}`} />
        </button>
      ) : (
        <div className="w-full flex justify-center py-2 text-gray-600">
          {collapsedIcon}
        </div>
      )}
      {(expanded || collapsed) && <div className="space-y-0.5">{children}</div>}
    </div>
  );
}

interface SidebarLinkProps {
  to: string;
  icon: ReactNode;
  label: string;
  collapsed: boolean;
  pathname: string;
  matcher?: (pathname: string) => boolean;
  exact?: boolean;
  onClick?: () => void;
}

export function SidebarLink({ to, icon, label, collapsed, pathname, matcher, exact, onClick }: SidebarLinkProps) {
  const active = matcher ? matcher(pathname) : exact ? pathname === to : pathname === to;
  return (
    <Link
      to={to}
      onClick={onClick}
      title={label}
      className={`w-full flex items-center gap-3 px-3 py-2 rounded-md transition-colors text-sm border-l-2 ${
        active
          ? 'bg-komgaPrimary/10 text-komgaPrimary font-medium border-komgaPrimary'
          : 'text-gray-400 border-transparent hover:bg-gray-800/40 hover:text-white'
      } ${collapsed ? 'md:justify-center md:px-0 md:border-l-0' : ''}`}
    >
      {icon}
      <span className={collapsed ? 'md:hidden' : 'block'}>{label}</span>
    </Link>
  );
}
