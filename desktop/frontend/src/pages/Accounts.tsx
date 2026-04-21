import { useEffect, useMemo, useState } from 'react';
import { Plus, RotateCw, Trash2, Upload, CheckCircle2, XCircle, Loader2 } from 'lucide-react';
import { Modal, Select, Input, InputNumber, Button, Form, App as AntApp, Space } from 'antd';
import { useAppStore } from '@/stores/appStore';
import * as api from '@/lib/api';

export default function Accounts() {
  const instances = useAppStore((s) => s.instances);
  const loadInstances = useAppStore((s) => s.loadInstances);
  const [showModal, setShowModal] = useState(false);
  const [showBulk, setShowBulk] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const { modal } = AntApp.useApp();

  useEffect(() => { loadInstances(); }, [loadInstances]);

  const existingNames = useMemo(() => new Set(instances.map((i) => i.name)), [instances]);

  const handleRestart = async (name: string) => {
    setBusy(name);
    try { await api.restartInstance(name); } catch (e) { console.error(e); }
    await loadInstances();
    setBusy(null);
  };

  const handleDelete = (name: string) => {
    modal.confirm({
      title: '确认删除',
      content: `确认删除实例 "${name}"？此操作不可撤销。`,
      okText: '删除',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk: async () => {
        try { await api.deleteInstance(name); await loadInstances(); } catch (e) { console.error(e); }
      },
    });
  };

  const btnStyle: React.CSSProperties = {
    padding: '8px 16px', borderRadius: 6, fontSize: 14, fontWeight: 500,
    cursor: 'pointer', border: '1px solid var(--border-color)', background: 'white',
    display: 'inline-flex', alignItems: 'center', gap: 8, transition: 'all 0.2s', flex: 1,
  };

  return (
    <div className="page-enter" style={{ flex: 1, overflowY: 'auto', background: '#ffffff' }}>
      <div style={{ padding: 40, maxWidth: 1100, margin: '0 auto', paddingBottom: 80 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 32 }}>
          <h1 style={{ fontSize: 24, fontWeight: 600 }}>实例管理</h1>
          <Space size={8}>
            <Button icon={<Upload size={16} />} size="large" onClick={() => setShowBulk(true)}>
              批量导入
            </Button>
            <Button type="primary" icon={<Plus size={16} />} size="large" onClick={() => setShowModal(true)}>
              添加实例
            </Button>
          </Space>
        </div>

        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))', gap: 20 }}>
          {instances.length === 0 ? (
            <div style={{ gridColumn: '1 / -1', padding: '48px 20px', textAlign: 'center', color: 'var(--text-muted)', fontSize: 14, border: '1px solid var(--border-color)', borderRadius: 12 }}>
              暂无实例，点击"添加实例"开始
            </div>
          ) : (
            instances.map((inst) => (
              <div
                key={inst.name}
                style={{ background: 'white', border: '1px solid var(--border-color)', borderRadius: 12, padding: 20, transition: 'transform 0.2s, box-shadow 0.2s', cursor: 'default' }}
                onMouseEnter={(e) => { e.currentTarget.style.transform = 'translateY(-2px)'; e.currentTarget.style.boxShadow = '0 10px 20px rgba(0,0,0,0.05)'; }}
                onMouseLeave={(e) => { e.currentTarget.style.transform = ''; e.currentTarget.style.boxShadow = ''; }}
              >
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 16 }}>
                  <div style={{ fontWeight: 600, fontSize: 16, display: 'flex', alignItems: 'center', gap: 8 }}>
                    <span style={{ width: 8, height: 8, borderRadius: '50%', display: 'inline-block', background: inst.healthy ? 'var(--success)' : 'var(--danger)', boxShadow: inst.healthy ? '0 0 8px var(--success)' : '0 0 8px var(--danger)' }} />
                    {inst.name}
                  </div>
                  <span style={{ padding: '2px 8px', borderRadius: 12, fontSize: 12, fontWeight: 500, border: '1px solid var(--border-color)', color: 'var(--text-muted)' }}>
                    {inst.type.charAt(0).toUpperCase() + inst.type.slice(1)}
                  </span>
                </div>
                {inst.email && (
                  <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 12, fontSize: 13 }}>
                    <span style={{ color: 'var(--text-muted)' }}>Email</span>
                    <span style={{ fontWeight: 500 }}>{inst.email}</span>
                  </div>
                )}
                <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 12, fontSize: 13 }}>
                  <span style={{ color: 'var(--text-muted)' }}>端口</span><span style={{ fontWeight: 500 }}>{inst.port}</span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 12, fontSize: 13 }}>
                  <span style={{ color: 'var(--text-muted)' }}>权重</span><span style={{ fontWeight: 500 }}>{inst.weight}</span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 12, fontSize: 13 }}>
                  <span style={{ color: 'var(--text-muted)' }}>总请求</span><span style={{ fontWeight: 500 }}>{inst.total_requests.toLocaleString()}</span>
                </div>
                {inst.last_error && (
                  <div style={{ fontSize: 12, color: 'var(--danger)', marginBottom: 12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>⚠ {inst.last_error}</div>
                )}
                <div style={{ borderTop: '1px solid var(--border-color)', paddingTop: 16, display: 'flex', gap: 8 }}>
                  <button onClick={() => handleRestart(inst.name)} disabled={busy === inst.name} style={btnStyle}>
                    <RotateCw size={14} className={busy === inst.name ? 'animate-spin' : ''} /> 重启
                  </button>
                  <button onClick={() => handleDelete(inst.name)} style={{ ...btnStyle, color: 'var(--danger)', borderColor: 'var(--danger)' }}>
                    <Trash2 size={14} /> 删除
                  </button>
                </div>
              </div>
            ))
          )}
        </div>

        <AddInstanceModal open={showModal} onClose={() => setShowModal(false)} onCreated={loadInstances} />
        <BulkImportModal
          open={showBulk}
          onClose={() => setShowBulk(false)}
          onCreated={loadInstances}
          existingNames={existingNames}
        />
      </div>
    </div>
  );
}

function AddInstanceModal({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: () => Promise<void> }) {
  const [form] = Form.useForm();
  const [type, setType] = useState('local');
  const [submitting, setSubmitting] = useState(false);
  const { message } = AntApp.useApp();

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      setSubmitting(true);
      await api.createInstance({ ...values, type });
      message.success('实例添加成功');
      await onCreated();
      form.resetFields();
      onClose();
    } catch (e: unknown) {
      if (e instanceof Error) message.error(e.message);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal
      title="添加实例"
      open={open}
      onCancel={onClose}
      onOk={handleSubmit}
      confirmLoading={submitting}
      okText="添加实例"
      cancelText="取消"
      width={520}
      destroyOnClose
    >
      <Form form={form} layout="vertical" style={{ marginTop: 16 }} initialValues={{ weight: 10, host: '127.0.0.1', grpc_port: 42100, server_port: 42100 }}>
        <Form.Item label="实例类型">
          <Select value={type} onChange={setType} options={[
            { value: 'local', label: 'Local (自动发现)' },
            { value: 'standalone', label: 'Standalone (独立进程)' },
            { value: 'manual', label: 'Manual (手动配置)' },
          ]} />
        </Form.Item>

        <Form.Item label="实例名称" name="name" rules={[{ required: true, message: '请填写实例名称' }]}>
          <Input placeholder="my-instance" />
        </Form.Item>

        <Form.Item label="权重" name="weight">
          <InputNumber min={1} max={100} style={{ width: 140 }} />
        </Form.Item>

        {type === 'manual' && (
          <>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
              <Form.Item label="Host" name="host"><Input /></Form.Item>
              <Form.Item label="gRPC Port" name="grpc_port"><InputNumber style={{ width: '100%' }} /></Form.Item>
            </div>
            <Form.Item label="CSRF Token" name="csrf_token"><Input /></Form.Item>
            <Form.Item label="API Key" name="api_key"><Input /></Form.Item>
          </>
        )}

        {type === 'standalone' && (
          <>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
              <Form.Item label="Email" name="email"><Input placeholder="user@example.com" /></Form.Item>
              <Form.Item label="密码" name="password"><Input.Password /></Form.Item>
            </div>
            <Form.Item label="Server Port" name="server_port"><InputNumber style={{ width: 160 }} /></Form.Item>
            <Form.Item label="API Key (可选)" name="api_key"><Input placeholder="留空则自动登录获取" /></Form.Item>
          </>
        )}
      </Form>
    </Modal>
  );
}

// ─── Bulk import ────────────────────────────────────────────────────────────

type ParsedAccount = {
  raw: string;
  email: string;
  password: string;
  weight: number;
  name: string;
  error?: string;
};

type ImportStatus = 'pending' | 'running' | 'ok' | 'fail';

type ImportRow = ParsedAccount & {
  status: ImportStatus;
  message?: string;
};

const sanitizeName = (s: string) =>
  s
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 32) || 'acct';

function parseAccounts(text: string, existingNames: Set<string>): ParsedAccount[] {
  const used = new Set(existingNames);
  const result: ParsedAccount[] = [];
  const lines = text.split(/\r?\n/);

  for (const original of lines) {
    const line = original.trim();
    if (!line || line.startsWith('#')) continue;

    // Accept ":" or "," or whitespace as separator. Normalize to ":".
    const normalized = line.replace(/[,\s]+/g, ':');
    const parts = normalized.split(':').filter(Boolean);

    const acct: ParsedAccount = {
      raw: original,
      email: parts[0] || '',
      password: parts[1] || '',
      weight: 10,
      name: '',
    };

    if (parts.length >= 3) {
      const w = Number(parts[2]);
      if (Number.isFinite(w) && w > 0) acct.weight = Math.floor(w);
    }
    if (!acct.email || !acct.password) {
      acct.error = '格式错误：每行至少需要 email 和 密码';
    } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(acct.email)) {
      acct.error = '邮箱格式无效';
    }

    // Generate a unique instance name from email username.
    const base = sanitizeName(acct.email.split('@')[0] || 'acct');
    let candidate = base;
    let i = 2;
    while (used.has(candidate)) {
      candidate = `${base}-${i++}`;
    }
    used.add(candidate);
    acct.name = candidate;

    result.push(acct);
  }
  return result;
}

function BulkImportModal({
  open,
  onClose,
  onCreated,
  existingNames,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: () => Promise<void>;
  existingNames: Set<string>;
}) {
  const [text, setText] = useState('');
  const [rows, setRows] = useState<ImportRow[]>([]);
  const [running, setRunning] = useState(false);
  const [done, setDone] = useState(false);
  const { message } = AntApp.useApp();

  const parsed = useMemo(() => parseAccounts(text, existingNames), [text, existingNames]);
  const validCount = parsed.filter((p) => !p.error).length;

  const handleParse = () => {
    setRows(parsed.map((p) => ({ ...p, status: p.error ? 'fail' : 'pending', message: p.error })));
    setDone(false);
  };

  const handleImport = async () => {
    if (rows.length === 0) {
      handleParse();
      return;
    }
    setRunning(true);
    setDone(false);
    // Run sequentially so we can show meaningful progress and avoid Firebase
    // throttling from too many parallel logins.
    for (let i = 0; i < rows.length; i++) {
      const row = rows[i];
      if (row.status === 'fail') continue;
      setRows((prev) => prev.map((r, idx) => (idx === i ? { ...r, status: 'running', message: '登录中…' } : r)));
      try {
        await api.createInstance({
          name: row.name,
          type: 'standalone',
          email: row.email,
          password: row.password,
          weight: row.weight,
          // server_port left as 0 → backend now auto-allocates a free port.
        });
        setRows((prev) => prev.map((r, idx) => (idx === i ? { ...r, status: 'ok', message: '已添加' } : r)));
      } catch (e: unknown) {
        const msg = e instanceof Error ? e.message : '未知错误';
        setRows((prev) => prev.map((r, idx) => (idx === i ? { ...r, status: 'fail', message: msg } : r)));
      }
    }
    setRunning(false);
    setDone(true);
    await onCreated();
    const ok = rows.filter((r) => r.status === 'ok').length;
    if (ok > 0) message.success(`成功导入 ${ok} 个账号`);
  };

  const reset = () => {
    setText('');
    setRows([]);
    setDone(false);
  };

  const handleClose = () => {
    if (running) return;
    reset();
    onClose();
  };

  return (
    <Modal
      title="批量导入账号"
      open={open}
      onCancel={handleClose}
      width={680}
      destroyOnClose
      maskClosable={!running}
      footer={[
        <Button key="cancel" onClick={handleClose} disabled={running}>
          {done ? '关闭' : '取消'}
        </Button>,
        rows.length === 0 ? (
          <Button key="parse" onClick={handleParse} disabled={!text.trim()}>
            解析 ({parsed.length} 行)
          </Button>
        ) : null,
        <Button
          key="import"
          type="primary"
          loading={running}
          onClick={handleImport}
          disabled={rows.length > 0 ? rows.every((r) => r.status === 'ok' || r.status === 'fail') && done : validCount === 0}
        >
          {rows.length === 0 ? `导入 ${validCount} 个账号` : done ? '已完成' : `开始导入 (${validCount})`}
        </Button>,
      ]}
    >
      {rows.length === 0 ? (
        <div style={{ marginTop: 8 }}>
          <div style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 8 }}>
            每行一个账号。支持格式：<br />
            <code>email:password</code> &nbsp;或&nbsp; <code>email,password</code> &nbsp;或&nbsp; <code>email password</code><br />
            可选第三段为权重：<code>email:password:5</code>。以 <code>#</code> 开头的行会被忽略。
          </div>
          <Input.TextArea
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder={`user1@example.com:password1\nuser2@example.com:password2:5\n# user3@example.com:disabled`}
            autoSize={{ minRows: 8, maxRows: 16 }}
            style={{ fontFamily: "'JetBrains Mono', 'Courier New', monospace", fontSize: 13 }}
          />
          {parsed.length > 0 && (
            <div style={{ marginTop: 12, fontSize: 13, color: 'var(--text-muted)' }}>
              将解析为 <strong>{parsed.length}</strong> 行，其中有效 <strong style={{ color: 'var(--success)' }}>{validCount}</strong> 个，
              无效 <strong style={{ color: 'var(--danger)' }}>{parsed.length - validCount}</strong> 个。
              所有账号将以 standalone 类型添加，自动分配空闲端口。
            </div>
          )}
        </div>
      ) : (
        <div style={{ marginTop: 8, maxHeight: 460, overflowY: 'auto' }}>
          {rows.map((r, idx) => (
            <div
              key={idx}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 12,
                padding: '8px 12px',
                borderBottom: '1px solid var(--border-color)',
                fontSize: 13,
              }}
            >
              <div style={{ width: 18, display: 'flex', justifyContent: 'center', flexShrink: 0 }}>
                {r.status === 'ok' && <CheckCircle2 size={16} color="var(--success)" />}
                {r.status === 'fail' && <XCircle size={16} color="var(--danger)" />}
                {r.status === 'running' && <Loader2 size={16} className="animate-spin" />}
                {r.status === 'pending' && <span style={{ width: 8, height: 8, borderRadius: '50%', background: 'var(--text-muted)', opacity: 0.4, display: 'inline-block' }} />}
              </div>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {r.email || <span style={{ color: 'var(--danger)' }}>{r.raw}</span>}
                  <span style={{ marginLeft: 8, color: 'var(--text-muted)', fontWeight: 400, fontSize: 12 }}>
                    → {r.name} (w={r.weight})
                  </span>
                </div>
                {r.message && (
                  <div
                    style={{
                      fontSize: 12,
                      color: r.status === 'fail' ? 'var(--danger)' : r.status === 'ok' ? 'var(--success)' : 'var(--text-muted)',
                      marginTop: 2,
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {r.message}
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </Modal>
  );
}
