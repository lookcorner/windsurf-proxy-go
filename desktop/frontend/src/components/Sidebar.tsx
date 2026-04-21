import {
  LayoutDashboard,
  Layers,
  Key,
  Settings as SettingsIcon,
  Wind,
} from 'lucide-react';
import { Menu } from 'antd';
import type { MenuProps } from 'antd';
import { useAppStore, type Page } from '@/stores/appStore';

const topItems: MenuProps['items'] = [
  { key: 'dashboard', icon: <LayoutDashboard size={18} />, label: '仪表盘' },
  { key: 'accounts', icon: <Layers size={18} />, label: '账号/实例管理' },
  { key: 'apikeys', icon: <Key size={18} />, label: 'API 密钥' },
];

const bottomItems: MenuProps['items'] = [
  { type: 'divider' },
  { key: 'settings', icon: <SettingsIcon size={18} />, label: '系统设置' },
];

export default function Sidebar() {
  const currentPage = useAppStore((s) => s.currentPage);
  const setPage = useAppStore((s) => s.setPage);

  const handleClick: MenuProps['onClick'] = (e) => {
    setPage(e.key as Page);
  };

  return (
    <aside
      style={{
        width: 240,
        minWidth: 240,
        background: 'var(--sidebar-bg)',
        backdropFilter: 'var(--glass-blur)',
        borderRight: '1px solid var(--border-color)',
        display: 'flex',
        flexDirection: 'column',
        zIndex: 10,
      }}
    >
      {/* macOS traffic-light safe zone + window drag region.
          With the custom TitleBar (UseToolbar=false) in main.go, the
          native red/yellow/green buttons now sit in the standard (higher)
          title-bar slot. A 22px strip is enough to keep the logo clear of
          them while staying draggable, matching the native title-bar feel. */}
      <div className="window-drag" style={{ height: 22, flexShrink: 0 }} />

      {/* Logo (also draggable — there are no interactive elements here) */}
      <div
        className="window-drag"
        style={{ display: 'flex', alignItems: 'center', gap: 12, fontWeight: 700, fontSize: 18, padding: '8px 24px 20px' }}
      >
        <div style={{ width: 32, height: 32, background: '#4f46e5', color: 'white', borderRadius: 8, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <Wind size={18} />
        </div>
        Windsurf Proxy
      </div>

      {/* Main Nav */}
      <Menu
        mode="inline"
        selectedKeys={[currentPage]}
        onClick={handleClick}
        items={topItems}
        style={{ border: 'none', background: 'transparent', flex: 1 }}
      />

      {/* Settings at bottom */}
      <Menu
        mode="inline"
        selectedKeys={[currentPage]}
        onClick={handleClick}
        items={bottomItems}
        style={{ border: 'none', background: 'transparent' }}
      />
    </aside>
  );
}
