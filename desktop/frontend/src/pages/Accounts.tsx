import { useEffect, useMemo, useState } from 'react';
import { Plus, RotateCw, Trash2, Upload, CheckCircle2, XCircle, Loader2 } from 'lucide-react';
import { Modal, Select, Input, InputNumber, Button, Form, App as AntApp, Space } from 'antd';
import { useAppStore } from '@/stores/appStore';
import * as api from '@/lib/api';
import { loginWithWindsurfOAuth, type WindsurfOAuthProvider } from '@/lib/firebaseAuth';

function authSourceLabel(source: string) {
  switch (source) {
    case 'password': return '邮箱密码';
    case 'refresh_token': return 'Refresh Token';
    case 'api_key': return 'API Key';
    case 'auto_discover': return '自动发现';
    default: return source || '-';
  }
}

function providerLabel(provider: string) {
  switch (provider) {
    case 'oauth': return 'OAuth';
    case 'email': return 'Email';
    case 'api_key': return 'API Key';
    default: return provider || '-';
  }
}

function formatLastUsed(unix: number) {
  if (!unix) return '-';
  return new Date(unix * 1000).toLocaleString();
}

function formatShortDateTime(unix: number) {
  if (!unix) return '-';
  const date = new Date(unix * 1000);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${pad(date.getMonth() + 1)}/${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function formatDurationUntil(unix: number) {
  if (!unix) return '-';
  const diff = Math.max(0, unix * 1000 - Date.now());
  const totalMinutes = Math.floor(diff / 60000);
  const days = Math.floor(totalMinutes / 1440);
  const hours = Math.floor((totalMinutes % 1440) / 60);
  const minutes = totalMinutes % 60;
  if (days > 0) return `${days}天${hours}小时`;
  if (hours > 0) return `${hours}小时${minutes}分钟`;
  return `${minutes}分钟`;
}

function formatUsageStatus(account: api.Account) {
  if (account.quota_exhausted || account.usage_status === 'exhausted') return '已耗尽';
  if (account.quota_low) return `额度偏低 (${formatQuotaPercent(account.lowest_quota_percent)})`;
  switch (account.usage_status) {
    case 'ok': return '已检查';
    case 'unavailable': return '检查失败';
    default: return '未知';
  }
}

function formatCredits(used: number, available: number) {
  if (!used && !available) return '-';
  return `${used} / ${available}`;
}

function formatQuotaPercent(value: number) {
  if (!value && value !== 0) return '-';
  return `${value}%`;
}

function quotaUsedPercent(remaining: number) {
  if (!remaining && remaining !== 0) return 0;
  return Math.max(0, Math.min(100, 100 - remaining));
}

function uniqModels(models: string[]) {
  return Array.from(new Set((models || []).filter(Boolean)));
}

const sanitizeName = (s: string) =>
  s
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 32) || 'acct';

export default function Accounts() {
  const accounts = useAppStore((s) => s.accounts);
  const instances = useAppStore((s) => s.instances);
  const loadAccounts = useAppStore((s) => s.loadAccounts);
  const loadInstances = useAppStore((s) => s.loadInstances);
  const [showAccountModal, setShowAccountModal] = useState(false);
  const [showInstanceModal, setShowInstanceModal] = useState(false);
  const [showBulk, setShowBulk] = useState(false);
  const [busyAccount, setBusyAccount] = useState<string | null>(null);
  const [busyInstance, setBusyInstance] = useState<string | null>(null);
  const [refreshingAllAccounts, setRefreshingAllAccounts] = useState(false);
  const { modal, message } = AntApp.useApp();

  const reloadAll = async () => {
    await Promise.all([loadAccounts(), loadInstances()]);
  };

  useEffect(() => {
    void Promise.all([loadAccounts(), loadInstances()]);
  }, [loadAccounts, loadInstances]);

  const existingNames = useMemo(
    () => new Set([...accounts.map((a) => a.name), ...instances.map((i) => i.name)]),
    [accounts, instances],
  );

  const handleRestart = async (name: string) => {
    setBusyInstance(name);
    try {
      await api.restartInstance(name);
    } catch (e) {
      console.error(e);
    }
    await loadInstances();
    setBusyInstance(null);
  };

  const handleDeleteInstance = (name: string) => {
    modal.confirm({
      title: '确认删除',
      content: `确认删除实例 "${name}"？此操作不可撤销。`,
      okText: '删除',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await api.deleteInstance(name);
          await loadInstances();
        } catch (e) {
          console.error(e);
        }
      },
    });
  };

  const handleDeleteAccount = (id: string, name: string) => {
    modal.confirm({
      title: '确认删除账号',
      content: `确认删除账号 "${name}"？如果仍有实例绑定，会被后端拒绝。`,
      okText: '删除',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await api.deleteAccount(id);
          await loadAccounts();
        } catch (e) {
          console.error(e);
        }
      },
    });
  };

  const handleRefreshAccount = async (account: api.Account) => {
    setBusyAccount(account.id);
    try {
      const result = await api.refreshAccount(account.id);
      const usageText = result.result.usage_status === 'exhausted' || result.result.quota_exhausted ? '，用量已耗尽' : '，用量已同步';
      message.success(`${account.name} 已完成${authSourceLabel(result.result.auth_source) || '凭证'}刷新${usageText}`);
      await loadAccounts();
    } catch (e) {
      console.error(e);
      if (e instanceof Error) message.error(e.message);
    } finally {
      setBusyAccount(null);
    }
  };

  const handleRefreshAllAccounts = async () => {
    setRefreshingAllAccounts(true);
    try {
      const result = await api.refreshAllAccounts();
      await loadAccounts();
      message.success(`批量刷新完成：成功 ${result.succeeded}，失败 ${result.failed}，已耗尽 ${result.exhausted}`);
    } catch (e) {
      console.error(e);
      if (e instanceof Error) message.error(e.message);
    } finally {
      setRefreshingAllAccounts(false);
    }
  };

  const btnStyle: React.CSSProperties = {
    padding: '8px 16px',
    borderRadius: 6,
    fontSize: 14,
    fontWeight: 500,
    cursor: 'pointer',
    border: '1px solid var(--border-color)',
    background: 'white',
    display: 'inline-flex',
    alignItems: 'center',
    gap: 8,
    transition: 'all 0.2s',
    flex: 1,
  };

  return (
    <div className="page-enter" style={{ flex: 1, overflowY: 'auto', background: '#ffffff' }}>
      <div style={{ padding: 40, maxWidth: 1100, margin: '0 auto', paddingBottom: 80 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 32 }}>
          <div>
            <h1 style={{ fontSize: 24, fontWeight: 600, margin: 0 }}>账号与实例</h1>
            <div style={{ marginTop: 8, fontSize: 13, color: 'var(--text-muted)' }}>
              请求会优先走账号池里的账号；local/manual 实例只负责承载请求，standalone 只在需要独立进程时再单独创建。
            </div>
          </div>
          <Space size={8}>
            <Button icon={<Upload size={16} />} size="large" onClick={() => setShowBulk(true)}>
              批量导入
            </Button>
            <Button icon={<Plus size={16} />} size="large" onClick={() => setShowAccountModal(true)}>
              添加账号
            </Button>
            <Button type="primary" icon={<Plus size={16} />} size="large" onClick={() => setShowInstanceModal(true)}>
              添加实例
            </Button>
          </Space>
        </div>

        <SectionTitle
          title="账号池"
          subtitle="账号集中管理并优先参与调度。OAuth、refresh token、API key 都在这里。"
        />
        <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 12 }}>
          <Button
            icon={<RotateCw size={16} className={refreshingAllAccounts ? 'animate-spin' : ''} />}
            loading={refreshingAllAccounts}
            onClick={() => void handleRefreshAllAccounts()}
          >
            批量刷新额度
          </Button>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))', gap: 20, marginBottom: 40 }}>
          {accounts.length === 0 ? (
            <EmptyCard text='暂无账号，点击"添加账号"开始' />
          ) : (
            accounts.map((account) => (
              <AccountCard
                key={account.id}
                account={account}
                busy={busyAccount === account.id}
                buttonStyle={btnStyle}
                onRefresh={() => void handleRefreshAccount(account)}
                onDelete={() => handleDeleteAccount(account.id, account.name)}
              />
            ))
          )}
        </div>

        <SectionTitle
          title="运行实例"
          subtitle="实例只负责承载请求和运行 LS。共享 worker 可以服务多个账号，不需要一号一实例。"
        />
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))', gap: 20 }}>
          {instances.length === 0 ? (
            <EmptyCard text='暂无实例，点击"添加实例"开始' />
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
                {inst.account_name ? <MetaRow label="绑定账号" value={inst.account_name} /> : null}
                {inst.email ? <MetaRow label="Email" value={inst.email} /> : null}
                {inst.auth_source ? <MetaRow label="认证来源" value={authSourceLabel(inst.auth_source)} /> : null}
                <MetaRow label="端口" value={String(inst.port)} />
                <MetaRow label="权重" value={String(inst.weight)} />
                <MetaRow label="总请求" value={inst.total_requests.toLocaleString()} />
                {inst.last_error && (
                  <div style={{ fontSize: 12, color: 'var(--danger)', marginBottom: 12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    ⚠ {inst.last_error}
                  </div>
                )}
                <div style={{ borderTop: '1px solid var(--border-color)', paddingTop: 16, display: 'flex', gap: 8 }}>
                  <button onClick={() => void handleRestart(inst.name)} disabled={busyInstance === inst.name} style={btnStyle}>
                    <RotateCw size={14} className={busyInstance === inst.name ? 'animate-spin' : ''} /> 重启
                  </button>
                  <button onClick={() => handleDeleteInstance(inst.name)} style={{ ...btnStyle, color: 'var(--danger)', borderColor: 'var(--danger)' }}>
                    <Trash2 size={14} /> 删除
                  </button>
                </div>
              </div>
            ))
          )}
        </div>

        <AddAccountModal open={showAccountModal} onClose={() => setShowAccountModal(false)} onCreated={loadAccounts} />
        <AddInstanceModal
          open={showInstanceModal}
          onClose={() => setShowInstanceModal(false)}
          onCreated={loadInstances}
          accounts={accounts}
        />
        <BulkImportModal
          open={showBulk}
          onClose={() => setShowBulk(false)}
          onCreated={reloadAll}
          existingNames={existingNames}
        />
      </div>
    </div>
  );
}

function AccountCard({
  account,
  busy,
  buttonStyle,
  onRefresh,
  onDelete,
}: {
  account: api.Account;
  busy: boolean;
  buttonStyle: React.CSSProperties;
  onRefresh: () => void;
  onDelete: () => void;
}) {
  const statusClass = account.quota_exhausted
    ? 'danger'
    : account.quota_low || !account.healthy
      ? 'warning'
      : 'ok';

  return (
    <div className="account-usage-card">
      <div className="account-card-header">
        <div>
          <div className="account-title-row">
            <span className={`account-health-dot ${statusClass}`} />
            <span className="account-name">{account.name}</span>
          </div>
          <div className="account-subtitle">
            {account.email || account.key_masked || account.id}
          </div>
        </div>
        <span className={`account-status-pill ${account.status === 'active' ? 'active' : 'inactive'}`}>
          {account.status}
        </span>
      </div>

      <div className="account-chip-row">
        <span>{providerLabel(account.provider)}</span>
        <span>{authSourceLabel(account.auth_source)}</span>
        {account.plan_name ? <span>{account.plan_name}</span> : null}
      </div>

      <div className="quota-panel">
        {!account.hide_daily_quota ? (
          <QuotaMeter
            label="每日额度用量"
            usedPercent={account.last_usage_check_unix ? quotaUsedPercent(account.daily_quota_remaining_percent) : 0}
            resetUnix={account.daily_quota_reset_unix}
            accent="orange"
          />
        ) : null}
        {!account.hide_weekly_quota ? (
          <QuotaMeter
            label="每周额度用量"
            usedPercent={account.last_usage_check_unix ? quotaUsedPercent(account.weekly_quota_remaining_percent) : 0}
            resetUnix={account.weekly_quota_reset_unix}
            accent="orange"
          />
        ) : null}
        <div className="quota-balance-row">
          <span>额外用量余额</span>
          <strong>{account.available_flow_credits ? `$${account.available_flow_credits.toFixed(2)}` : '$0.00'}</strong>
        </div>
      </div>

      <div className="quota-cycle-box">
        <strong>账号状态：{formatUsageStatus(account)}</strong>
        <span>
          最近检查：{formatLastUsed(account.last_usage_check_unix)}
        </span>
      </div>

      <ModelCatalogPanel account={account} />

      <div className="account-meta-grid">
        <MetaRow label="API Server" value={account.api_server || '-'} />
        <MetaRow label="Proxy" value={account.proxy || '直连'} />
        <MetaRow label="活跃/总请求" value={`${account.active_requests} / ${account.total_requests}`} />
        <MetaRow label="连续失败" value={String(account.consecutive_failures)} />
        <MetaRow label="Prompt Credits" value={formatCredits(account.used_prompt_credits, account.available_prompt_credits)} />
        <MetaRow label="最近使用" value={formatLastUsed(account.last_used_unix)} />
      </div>

      {account.usage_error || account.last_error ? (
        <div className="account-error-line">
          {account.usage_error || account.last_error}
        </div>
      ) : null}

      <div style={{ borderTop: '1px solid var(--border-color)', paddingTop: 16, display: 'flex', gap: 8 }}>
        <button onClick={onRefresh} disabled={busy} style={buttonStyle}>
          <RotateCw size={14} className={busy ? 'animate-spin' : ''} />
          {account.has_api_key ? '刷新凭证/用量' : '手动登录/检查'}
        </button>
        <button onClick={onDelete} style={{ ...buttonStyle, color: 'var(--danger)', borderColor: 'var(--danger)' }}>
          <Trash2 size={14} /> 删除账号
        </button>
      </div>
    </div>
  );
}

function ModelCatalogPanel({ account }: { account: api.Account }) {
  const manual = uniqModels(account.available_models || []);
  const synced = uniqModels(account.synced_available_models || []);
  const blocked = uniqModels(account.blocked_models || []);

  if (manual.length === 0 && synced.length === 0 && blocked.length === 0 && !account.last_model_sync_unix) {
    return null;
  }

  return (
    <div style={{ marginTop: 16, padding: 14, borderRadius: 14, border: '1px solid rgba(15,23,42,0.08)', background: 'rgba(248,250,252,0.72)' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'baseline', marginBottom: 10 }}>
        <strong style={{ fontSize: 13 }}>模型目录</strong>
        <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
          {account.last_model_sync_unix ? `自动同步：${formatLastUsed(account.last_model_sync_unix)}` : '未自动同步'}
        </span>
      </div>

      {manual.length > 0 ? <ModelTagRow label="手动允许" tone="manual" models={manual} /> : null}
      {synced.length > 0 ? <ModelTagRow label="自动同步" tone="synced" models={synced} /> : null}
      {blocked.length > 0 ? <ModelTagRow label="手动屏蔽" tone="blocked" models={blocked} /> : null}
    </div>
  );
}

function ModelTagRow({
  label,
  tone,
  models,
}: {
  label: string;
  tone: 'manual' | 'synced' | 'blocked';
  models: string[];
}) {
  const palette = tone === 'blocked'
    ? { text: '#b42318', bg: 'rgba(254,228,226,0.9)', border: 'rgba(240,68,56,0.18)' }
    : tone === 'manual'
      ? { text: '#0f172a', bg: 'rgba(226,232,240,0.9)', border: 'rgba(148,163,184,0.28)' }
      : { text: '#065f46', bg: 'rgba(209,250,229,0.92)', border: 'rgba(16,185,129,0.18)' };

  return (
    <div style={{ display: 'grid', gridTemplateColumns: '88px 1fr', gap: 10, alignItems: 'start', marginTop: 8 }}>
      <span style={{ fontSize: 12, color: 'var(--text-muted)', paddingTop: 4 }}>{label}</span>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
        {models.map((model) => (
          <span
            key={`${label}-${model}`}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              padding: '5px 10px',
              borderRadius: 999,
              fontSize: 12,
              fontWeight: 600,
              color: palette.text,
              background: palette.bg,
              border: `1px solid ${palette.border}`,
            }}
          >
            {model}
          </span>
        ))}
      </div>
    </div>
  );
}

function QuotaMeter({
  label,
  usedPercent,
  resetUnix,
  accent,
}: {
  label: string;
  usedPercent: number;
  resetUnix: number;
  accent: 'orange' | 'green';
}) {
  const clamped = Math.max(0, Math.min(100, usedPercent));
  return (
    <div className="quota-meter">
      <div className="quota-meter-label">
        <span>{label}</span>
        <strong>{Math.round(clamped)}%</strong>
      </div>
      <div className="quota-track">
        <div className={`quota-fill ${accent}`} style={{ width: `${clamped}%` }} />
      </div>
      <div className="quota-reset">
        重置：{resetUnix ? `${formatDurationUntil(resetUnix)}（${formatShortDateTime(resetUnix)}）` : '-'}
      </div>
    </div>
  );
}

function SectionTitle({ title, subtitle }: { title: string; subtitle: string }) {
  return (
    <div style={{ marginBottom: 16 }}>
      <div style={{ fontSize: 16, fontWeight: 600 }}>{title}</div>
      <div style={{ marginTop: 4, fontSize: 13, color: 'var(--text-muted)' }}>{subtitle}</div>
    </div>
  );
}

function MetaRow({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 12, fontSize: 13, gap: 16 }}>
      <span style={{ color: 'var(--text-muted)', flexShrink: 0 }}>{label}</span>
      <span style={{ fontWeight: 500, textAlign: 'right', wordBreak: 'break-all' }}>{value}</span>
    </div>
  );
}

function EmptyCard({ text }: { text: string }) {
  return (
    <div
      style={{
        gridColumn: '1 / -1',
        padding: '48px 20px',
        textAlign: 'center',
        color: 'var(--text-muted)',
        fontSize: 14,
        border: '1px solid var(--border-color)',
        borderRadius: 12,
      }}
    >
      {text}
    </div>
  );
}

function AddAccountModal({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: () => Promise<void> }) {
  const [form] = Form.useForm();
  const [submitting, setSubmitting] = useState(false);
  const [oauthBusy, setOAuthBusy] = useState<WindsurfOAuthProvider | null>(null);
  const { message } = AntApp.useApp();

  const handleOAuthLogin = async (provider: WindsurfOAuthProvider) => {
    try {
      setOAuthBusy(provider);
      const { email, refreshToken } = await loginWithWindsurfOAuth(provider);
      const currentName = form.getFieldValue('name');
      const nextName = currentName || sanitizeName((email.split('@')[0] || provider) + '-oauth');
      form.setFieldsValue({
        name: nextName,
        email,
        password: '',
        firebase_refresh_token: refreshToken,
      });
      message.success(`${provider === 'google' ? 'Google' : 'GitHub'} OAuth 登录成功`);
    } catch (e: unknown) {
      if (e instanceof Error) message.error(e.message);
    } finally {
      setOAuthBusy(null);
    }
  };

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      setSubmitting(true);
      await api.createAccount(values);
      message.success('账号添加成功');
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
      title="添加账号"
      open={open}
      onCancel={onClose}
      onOk={() => void handleSubmit()}
      confirmLoading={submitting}
      okText="添加账号"
      cancelText="取消"
      width={560}
      destroyOnClose
    >
      <Form form={form} layout="vertical" style={{ marginTop: 16 }}>
        <Space size={8} style={{ marginBottom: 12, width: '100%' }}>
          <Button loading={oauthBusy === 'google'} disabled={submitting || oauthBusy !== null} onClick={() => void handleOAuthLogin('google')}>
            Google 登录
          </Button>
          <Button loading={oauthBusy === 'github'} disabled={submitting || oauthBusy !== null} onClick={() => void handleOAuthLogin('github')}>
            GitHub 登录
          </Button>
        </Space>

        <Form.Item label="账号名称" name="name" rules={[{ required: true, message: '请填写账号名称' }]}>
          <Input placeholder="my-pro-account" />
        </Form.Item>

        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
          <Form.Item label="Email" name="email">
            <Input placeholder="user@example.com" />
          </Form.Item>
          <Form.Item label="密码" name="password">
            <Input.Password />
          </Form.Item>
        </div>

        <Form.Item label="Firebase Refresh Token" name="firebase_refresh_token">
          <Input.Password placeholder="可直接粘贴 OAuth 登录后拿到的 refresh token" />
        </Form.Item>

        <Form.Item label="API Key" name="api_key">
          <Input placeholder="如果已有可直接填写" />
        </Form.Item>

        <Form.Item label="API Server (可选)" name="api_server">
          <Input placeholder="https://server.self-serve.windsurf.com" />
        </Form.Item>

        <Form.Item label="Proxy (可选)" name="proxy" tooltip="这个账号的请求会走该出口；相同 proxy 的账号共享一个 standalone worker。">
          <Input placeholder="http://user:pass@127.0.0.1:7890 或 socks5://127.0.0.1:1080" />
        </Form.Item>
      </Form>
    </Modal>
  );
}

function AddInstanceModal({
  open,
  onClose,
  onCreated,
  accounts,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: () => Promise<void>;
  accounts: api.Account[];
}) {
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
      onOk={() => void handleSubmit()}
      confirmLoading={submitting}
      okText="添加实例"
      cancelText="取消"
      width={560}
      destroyOnClose
    >
      <Form
        form={form}
        layout="vertical"
        style={{ marginTop: 16 }}
        initialValues={{ weight: 10, host: '127.0.0.1', grpc_port: 42100, server_port: 42100 }}
      >
        <Form.Item label="实例类型">
          <Select
            value={type}
            onChange={setType}
            options={[
              { value: 'local', label: 'Local (自动发现)' },
              { value: 'standalone', label: 'Standalone (独立进程)' },
              { value: 'manual', label: 'Manual (手动配置)' },
            ]}
          />
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
            <Form.Item label="Proxy 标记 (可选)" name="proxy" tooltip="仅用于声明这个 manual worker 的出口；不会改造已有 LS 进程。">
              <Input placeholder="http://127.0.0.1:7890" />
            </Form.Item>
          </>
        )}

        {type === 'standalone' && (
          <>
            <Form.Item
              label="启动账号（可选）"
              name="account_id"
              tooltip="仅用于启动 standalone 的初始元数据；请求会按账号池动态注入 API Key。"
            >
              <Select
                allowClear
                placeholder={accounts.length === 0 ? '暂无账号也可创建共享实例' : '可选：选择一个账号作为启动账号'}
                options={accounts.map((account) => ({
                  value: account.id,
                  label: `${account.name}${account.email ? ` · ${account.email}` : ''}`,
                }))}
              />
            </Form.Item>

            <Form.Item label="Server Port" name="server_port">
              <InputNumber style={{ width: 180 }} />
            </Form.Item>
            <Form.Item label="Proxy (可选)" name="proxy" tooltip="Standalone 进程启动时会注入 HTTP_PROXY/HTTPS_PROXY。留空时会使用启动账号的 proxy。">
              <Input placeholder="http://user:pass@127.0.0.1:7890 或 socks5://127.0.0.1:1080" />
            </Form.Item>
          </>
        )}
      </Form>
    </Modal>
  );
}

type ParsedAccount = {
  raw: string;
  email: string;
  password: string;
  name: string;
  error?: string;
};

type ImportStatus = 'pending' | 'running' | 'ok' | 'fail';

type ImportRow = ParsedAccount & {
  status: ImportStatus;
  message?: string;
};

function parseAccounts(text: string, existingNames: Set<string>): ParsedAccount[] {
  const used = new Set(existingNames);
  const result: ParsedAccount[] = [];
  const lines = text.split(/\r?\n/);

  for (const original of lines) {
    const line = original.trim();
    if (!line || line.startsWith('#')) continue;

    const normalized = line.replace(/[,\s]+/g, ':');
    const parts = normalized.split(':').filter(Boolean);

    const acct: ParsedAccount = {
      raw: original,
      email: parts[0] || '',
      password: parts[1] || '',
      name: '',
    };

    if (!acct.email || !acct.password) {
      acct.error = '格式错误：每行至少需要 email 和 密码';
    } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(acct.email)) {
      acct.error = '邮箱格式无效';
    }

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

    let okCount = 0;
    for (let i = 0; i < rows.length; i++) {
      const row = rows[i];
      if (row.status === 'fail') continue;

      setRows((prev) => prev.map((r, idx) => (idx === i ? { ...r, status: 'running', message: '创建账号中…' } : r)));

      try {
        await api.createAccount({
          name: row.name,
          email: row.email,
          password: row.password,
        });

        okCount++
        setRows((prev) => prev.map((r, idx) => (idx === i ? { ...r, status: 'ok', message: '账号已加入账号池' } : r)));
      } catch (e: unknown) {
        const msg = e instanceof Error ? e.message : '未知错误';
        setRows((prev) => prev.map((r, idx) => (idx === i ? { ...r, status: 'fail', message: msg } : r)));
      }
    }

    setRunning(false);
    setDone(true);
    await onCreated();
    if (okCount > 0) message.success(`成功导入 ${okCount} 个账号`);
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
          onClick={() => void handleImport()}
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
            以 <code>#</code> 开头的行会被忽略。
          </div>
          <Input.TextArea
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder={`user1@example.com:password1\nuser2@example.com:password2\n# user3@example.com:disabled`}
            autoSize={{ minRows: 8, maxRows: 16 }}
            style={{ fontFamily: "'JetBrains Mono', 'Courier New', monospace", fontSize: 13 }}
          />
          {parsed.length > 0 && (
            <div style={{ marginTop: 12, fontSize: 13, color: 'var(--text-muted)' }}>
              将解析为 <strong>{parsed.length}</strong> 行，其中有效 <strong style={{ color: 'var(--success)' }}>{validCount}</strong> 个，
              无效 <strong style={{ color: 'var(--danger)' }}>{parsed.length - validCount}</strong> 个。
              导入只会创建账号，不会再为每个账号自动拉起 standalone 实例。
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
                  <span style={{ marginLeft: 8, color: 'var(--text-muted)', fontWeight: 400, fontSize: 12 }}>→ {r.name}</span>
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
