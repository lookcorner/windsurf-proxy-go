import { useEffect } from 'react';
import { ArrowLeft, RefreshCw } from 'lucide-react';
import { Button, Table, Tag } from 'antd';
import { useAppStore } from '@/stores/appStore';

export default function RequestHistory() {
  const requestHistory = useAppStore((s) => s.requestHistory);
  const loadRequestHistory = useAppStore((s) => s.loadRequestHistory);
  const setPage = useAppStore((s) => s.setPage);

  useEffect(() => {
    loadRequestHistory();
    const interval = setInterval(loadRequestHistory, 5000);
    return () => clearInterval(interval);
  }, [loadRequestHistory]);

  const columns = [
    {
      title: '时间',
      dataIndex: 'time_str',
      key: 'time_str',
      width: 180,
      render: (v: string) => <span style={{ fontFamily: 'monospace', fontSize: 13 }}>{v}</span>,
    },
    {
      title: '模型',
      dataIndex: 'model',
      key: 'model',
      render: (v: string) => <Tag color={v ? 'blue' : 'default'}>{v || '未识别'}</Tag>,
    },
    {
      title: '实例',
      dataIndex: 'instance',
      key: 'instance',
      render: (v: string) => v ? <Tag>{v}</Tag> : <span style={{ color: 'var(--text-muted)' }}>-</span>,
    },
    {
      title: '请求账号',
      dataIndex: 'account',
      key: 'account',
      render: (v: string, record: { instance?: string }) => {
        if (v) return <Tag color="gold">{v}</Tag>;
        return <span style={{ color: 'var(--text-muted)' }}>{record.instance ? '本地凭证' : '未路由'}</span>;
      },
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => (
        <Tag color={v === 'ok' ? 'success' : 'error'}>{v === 'ok' ? '成功' : '失败'}</Tag>
      ),
    },
    {
      title: '类型',
      dataIndex: 'stream',
      key: 'stream',
      width: 90,
      render: (v: boolean) => <Tag color={v ? 'purple' : 'default'}>{v ? 'Stream' : 'Sync'}</Tag>,
    },
    {
      title: '耗时',
      dataIndex: 'duration_ms',
      key: 'duration_ms',
      width: 100,
      render: (v: number) => `${v}ms`,
      sorter: (a: { duration_ms: number }, b: { duration_ms: number }) => a.duration_ms - b.duration_ms,
    },
    {
      title: 'Turns',
      dataIndex: 'turns',
      key: 'turns',
      width: 90,
      render: (v: number) => v.toLocaleString(),
      sorter: (a: { turns: number }, b: { turns: number }) => a.turns - b.turns,
    },
    {
      title: 'Prompt Chars',
      dataIndex: 'prompt_chars',
      key: 'prompt_chars',
      width: 120,
      render: (v: number) => v.toLocaleString(),
      sorter: (a: { prompt_chars: number }, b: { prompt_chars: number }) => a.prompt_chars - b.prompt_chars,
    },
    {
      title: 'Prompt',
      dataIndex: 'prompt_tokens',
      key: 'prompt_tokens',
      width: 90,
      render: (v: number) => v.toLocaleString(),
    },
    {
      title: 'Completion',
      dataIndex: 'completion_tokens',
      key: 'completion_tokens',
      width: 110,
      render: (v: number) => v.toLocaleString(),
    },
    {
      title: 'Total',
      dataIndex: 'total_tokens',
      key: 'total_tokens',
      width: 90,
      render: (v: number) => <span style={{ fontWeight: 600 }}>{v.toLocaleString()}</span>,
    },
    {
      title: '错误信息',
      dataIndex: 'error',
      key: 'error',
      ellipsis: true,
      render: (v: string | null) => v ? <span style={{ color: 'var(--danger)', fontSize: 12 }}>{v}</span> : '-',
    },
  ];

  // Summary stats
  const totalReqs = requestHistory.length;
  const successReqs = requestHistory.filter((r) => r.status === 'ok').length;
  const avgDuration = totalReqs > 0 ? Math.round(requestHistory.reduce((s, r) => s + r.duration_ms, 0) / totalReqs) : 0;
  const totalTokens = requestHistory.reduce((s, r) => s + r.total_tokens, 0);

  // Model distribution
  const modelCounts: Record<string, number> = {};
  requestHistory.forEach((r) => {
    if (!r.model) return;
    modelCounts[r.model] = (modelCounts[r.model] || 0) + 1;
  });
  const modelEntries = Object.entries(modelCounts).sort((a, b) => b[1] - a[1]);

  return (
    <div className="page-enter" style={{ flex: 1, overflowY: 'auto', background: '#ffffff' }}>
      <div style={{ padding: 40, maxWidth: 1200, margin: '0 auto', paddingBottom: 80 }}>
        {/* Header */}
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 32 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
            <Button icon={<ArrowLeft size={16} />} onClick={() => setPage('dashboard')} />
            <h1 style={{ fontSize: 24, fontWeight: 600 }}>请求历史</h1>
          </div>
          <Button icon={<RefreshCw size={14} />} onClick={loadRequestHistory}>
            刷新
          </Button>
        </div>

        {/* Summary Cards */}
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 16, marginBottom: 24 }}>
          <div style={{ background: '#f8fafc', border: '1px solid var(--border-color)', borderRadius: 12, padding: 20 }}>
            <div style={{ fontSize: 24, fontWeight: 700 }}>{totalReqs}</div>
            <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>总请求</div>
          </div>
          <div style={{ background: '#f8fafc', border: '1px solid var(--border-color)', borderRadius: 12, padding: 20 }}>
            <div style={{ fontSize: 24, fontWeight: 700, color: '#16a34a' }}>{successReqs}</div>
            <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>成功</div>
          </div>
          <div style={{ background: '#f8fafc', border: '1px solid var(--border-color)', borderRadius: 12, padding: 20 }}>
            <div style={{ fontSize: 24, fontWeight: 700 }}>{avgDuration}ms</div>
            <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>平均耗时</div>
          </div>
          <div style={{ background: '#f8fafc', border: '1px solid var(--border-color)', borderRadius: 12, padding: 20 }}>
            <div style={{ fontSize: 24, fontWeight: 700 }}>{totalTokens.toLocaleString()}</div>
            <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>总 Tokens</div>
          </div>
        </div>

        {/* Model distribution */}
        {modelEntries.length > 0 && (
          <div style={{ marginBottom: 24 }}>
            <div style={{ fontSize: 14, fontWeight: 600, marginBottom: 12 }}>模型分布</div>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
              {modelEntries.map(([model, count]) => (
                <Tag key={model} color="blue" style={{ fontSize: 13, padding: '4px 12px' }}>
                  {model}: {count}
                </Tag>
              ))}
            </div>
          </div>
        )}

        {/* Request Table */}
        <Table
          columns={columns}
          dataSource={requestHistory}
          rowKey="id"
          pagination={{ pageSize: 50, showTotal: (total) => `共 ${total} 条请求` }}
          size="middle"
          locale={{ emptyText: '暂无请求记录' }}
          scroll={{ x: 1440 }}
        />
      </div>
    </div>
  );
}
