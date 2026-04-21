import { useEffect, useRef } from 'react';
import { RefreshCw } from 'lucide-react';
import { Button, Table, Tag, Card, Statistic } from 'antd';
import { useAppStore } from '@/stores/appStore';
import { getApiBase } from '@/lib/api';

function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d}d ${String(h).padStart(2, '0')}h ${String(m).padStart(2, '0')}m`;
  if (h > 0) return `${h}h ${String(m).padStart(2, '0')}m`;
  return `${m}m`;
}

export default function Dashboard() {
  const stats = useAppStore((s) => s.stats);
  const instances = useAppStore((s) => s.instances);
  const logs = useAppStore((s) => s.logs);
  const loadStats = useAppStore((s) => s.loadStats);
  const loadInstances = useAppStore((s) => s.loadInstances);
  const setPage = useAppStore((s) => s.setPage);
  const logRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    loadStats();
    loadInstances();
    const interval = setInterval(() => { loadStats(); loadInstances(); }, 5000);
    return () => clearInterval(interval);
  }, [loadStats, loadInstances]);

  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [logs]);

  const columns = [
    { title: '名称', dataIndex: 'name', key: 'name', render: (v: string) => <span style={{ fontWeight: 600 }}>{v}</span> },
    { title: '类型', dataIndex: 'type', key: 'type', render: (v: string) => <Tag>{v}</Tag> },
    { title: '健康状态', dataIndex: 'healthy', key: 'healthy', render: (v: boolean) => <Tag color={v ? 'success' : 'error'}>{v ? 'Healthy' : 'Unhealthy'}</Tag> },
    { title: '活跃连接', dataIndex: 'active_connections', key: 'active_connections' },
    { title: '总请求', dataIndex: 'total_requests', key: 'total_requests', render: (v: number) => v.toLocaleString() },
    { title: '权重', dataIndex: 'weight', key: 'weight' },
  ];

  return (
    <div className="page-enter" style={{ flex: 1, overflowY: 'auto', background: '#ffffff' }}>
      <div style={{ padding: 40, maxWidth: 1100, margin: '0 auto', paddingBottom: 80 }}>
        {/* Header */}
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 32 }}>
          <h1 style={{ fontSize: 24, fontWeight: 600 }}>服务仪表盘</h1>
          <Button icon={<RefreshCw size={14} />} onClick={() => { loadStats(); loadInstances(); }}>
            刷新数据
          </Button>
        </div>

        {/* Status Bar */}
        <div style={{
          background: '#fdfdfd', border: '1px solid var(--border-color)', padding: '16px 24px',
          borderRadius: 12, display: 'flex', alignItems: 'center', gap: 40, marginBottom: 24,
        }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <span style={{ fontSize: 12, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>运行状态</span>
            <span style={{ fontSize: 14, fontWeight: 500, display: 'flex', alignItems: 'center', gap: 8 }}>
              <span style={{ width: 8, height: 8, borderRadius: '50%', background: 'var(--success)', boxShadow: '0 0 8px var(--success)', display: 'inline-block' }} />
              服务运行中
            </span>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <span style={{ fontSize: 12, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>监听地址</span>
            <span style={{ fontSize: 14, fontWeight: 500 }}>{stats ? (() => { const u = new URL(getApiBase()); return u.host; })() : '0.0.0.0:8000'}</span>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <span style={{ fontSize: 12, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>运行时间</span>
            <span style={{ fontSize: 14, fontWeight: 500 }}>{stats ? formatUptime(stats.uptime_seconds) : '--'}</span>
          </div>
        </div>

        {/* Stat Cards */}
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 20, marginBottom: 32 }}>
          <Card size="small" hoverable onClick={() => setPage('requests')} style={{ cursor: 'pointer' }}>
            <Statistic title="总请求数" value={stats?.total_requests ?? 0} suffix={<span style={{ fontSize: 12, color: 'var(--text-muted)' }}>→ 详情</span>} />
          </Card>
          <Card size="small"><Statistic title="活跃连接" value={stats?.active_connections ?? 0} /></Card>
          <Card size="small"><Statistic title="健康实例" value={stats ? `${stats.healthy_count} / ${stats.instance_count}` : '0 / 0'} /></Card>
          <Card size="small" hoverable onClick={() => setPage('models_view')} style={{ cursor: 'pointer' }}>
            <Statistic title="已注册模型" value={stats?.model_count ?? 0} suffix={<span style={{ fontSize: 12, color: 'var(--text-muted)' }}>→ 查看</span>} />
          </Card>
        </div>

        {/* Instance Table */}
        <div style={{ fontSize: 16, fontWeight: 600, marginBottom: 16 }}>实例运行状态</div>
        <Table
          columns={columns}
          dataSource={instances}
          rowKey="name"
          pagination={false}
          size="middle"
          locale={{ emptyText: '暂无实例配置' }}
          style={{ marginBottom: 24 }}
        />

        {/* Logs */}
        <div style={{ fontSize: 16, fontWeight: 600, marginBottom: 16 }}>实时运行日志</div>
        <div
          ref={logRef}
          style={{
            background: '#0d1117', color: '#e6edf3', padding: 20, borderRadius: 12,
            fontFamily: "'JetBrains Mono', 'Courier New', monospace", fontSize: 12,
            height: 200, overflowY: 'auto', lineHeight: 1.6,
          }}
        >
          {logs.length === 0 ? (
            <div style={{ color: '#8b949e' }}>Waiting for log events...</div>
          ) : (
            logs.map((log, i) => (
              <div key={i} style={{ marginBottom: 4 }}>
                <span style={{ color: '#8b949e', marginRight: 8 }}>{log.time}</span>
                <span style={{ color: log.level === 'ERROR' ? '#f85149' : log.level === 'WARNING' ? '#d29922' : '#58a6ff' }}>
                  [{log.level}]
                </span>{' '}
                {log.message}
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
