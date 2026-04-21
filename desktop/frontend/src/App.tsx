import { useEffect } from 'react';
import { ConfigProvider, App as AntApp } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import Sidebar from '@/components/Sidebar';
import Dashboard from '@/pages/Dashboard';
import Accounts from '@/pages/Accounts';
import ApiKeys from '@/pages/ApiKeys';
import Settings from '@/pages/Settings';
import RequestHistory from '@/pages/RequestHistory';
import ModelsView from '@/pages/ModelsView';
import { useAppStore } from '@/stores/appStore';
import { setApiPort, getApiBase } from '@/lib/api';
import { GetStatus } from '../wailsjs/go/main/App';

function App() {
  const currentPage = useAppStore((s) => s.currentPage);
  const addLog = useAppStore((s) => s.addLog);
  const config = useAppStore((s) => s.config);

  // Bootstrap the API port from the native app before the first HTTP request.
  const loadConfig = useAppStore((s) => s.loadConfig);
  useEffect(() => {
    let cancelled = false;

    const bootstrap = async () => {
      try {
        const status = await GetStatus();
        const port = Number(status?.port);
        if (!cancelled && Number.isFinite(port) && port > 0) {
          setApiPort(port);
        }
      } catch {
        // Browser/dev mode does not expose Wails bindings.
      }

      await loadConfig();
    };

    bootstrap();

    return () => {
      cancelled = true;
    };
  }, [loadConfig]);

  useEffect(() => {
    if (config?.server?.port) {
      setApiPort(config.server.port);
    }
  }, [config]);

  // Connect to WebSocket log stream
  useEffect(() => {
    let ws: WebSocket | null = null;
    let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;
    let pingInterval: ReturnType<typeof setInterval> | null = null;
    let isMounted = true;

    const connect = () => {
      if (!isMounted) return;

      const wsBase = getApiBase().replace(/^http/, 'ws');
      ws = new WebSocket(`${wsBase}/api/logs`);
      ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data);
          addLog({ time: data.time, level: data.level, message: data.message });
        } catch {
          // ignore malformed messages
        }
      };
      ws.onopen = () => {
        if (!isMounted) {
          ws?.close();
          return;
        }
        pingInterval = setInterval(() => {
          if (ws?.readyState === WebSocket.OPEN && isMounted) {
            ws.send('ping');
          }
        }, 30000);
      };
      ws.onclose = () => {
        if (!isMounted) return;
        if (pingInterval) clearInterval(pingInterval);
        reconnectTimeout = setTimeout(() => {
          if (isMounted) connect();
        }, 3000);
      };
    };

    connect();

    return () => {
      isMounted = false;
      if (ws) ws.close();
      if (reconnectTimeout) clearTimeout(reconnectTimeout);
      if (pingInterval) clearInterval(pingInterval);
    };
  }, []);

  const renderPage = () => {
    switch (currentPage) {
      case 'dashboard':
        return <Dashboard />;
      case 'accounts':
        return <Accounts />;
      case 'apikeys':
        return <ApiKeys />;
      case 'settings':
        return <Settings />;
      case 'requests':
        return <RequestHistory />;
      case 'models_view':
        return <ModelsView />;
    }
  };

  return (
    <div style={{ display: 'flex', width: '100vw', height: '100vh' }}>
      <ConfigProvider
        locale={zhCN}
        theme={{
          token: {
            colorPrimary: '#4f46e5',
            borderRadius: 6,
            fontFamily: "'Inter', -apple-system, BlinkMacSystemFont, sans-serif",
          },
        }}
      >
        <AntApp style={{ display: 'flex', width: '100%', height: '100%' }}>
          <Sidebar />
          <main style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden', background: '#ffffff', position: 'relative' }}>
            {renderPage()}
          </main>
        </AntApp>
      </ConfigProvider>
    </div>
  );
}

export default App
