import { useEffect, useState } from 'react';
import { Plus, Copy, MoreHorizontal, Check, X } from 'lucide-react';
import { Modal, Input, InputNumber, Form, Select, Switch, Button, App as AntApp } from 'antd';
import { useAppStore } from '@/stores/appStore';
import * as api from '@/lib/api';
import { getApiBase } from '@/lib/api';

export default function ApiKeys() {
  const apiKeys = useAppStore((s) => s.apiKeys);
  const loadKeys = useAppStore((s) => s.loadKeys);
  const [showModal, setShowModal] = useState(false);
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const [newKey, setNewKey] = useState<string | null>(null);
  const { modal } = AntApp.useApp();

  useEffect(() => { loadKeys(); }, [loadKeys]);

  const handleCopy = (text: string, id: string) => {
    navigator.clipboard.writeText(text);
    setCopiedId(id);
    setTimeout(() => setCopiedId(null), 1500);
  };

  const handleDelete = (keyId: string, name: string) => {
    modal.confirm({
      title: '确认删除',
      content: `确认删除 API Key "${name}"？`,
      okText: '删除',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk: async () => {
        try { await api.deleteKey(keyId); await loadKeys(); } catch (e) { console.error(e); }
      },
    });
  };

  return (
    <div className="page-enter" style={{ flex: 1, overflowY: 'auto', background: '#ffffff' }}>
      <div style={{ padding: 40, maxWidth: 1100, margin: '0 auto', paddingBottom: 80 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 32 }}>
          <h1 style={{ fontSize: 24, fontWeight: 600 }}>API 密钥管理</h1>
          <Button type="primary" icon={<Plus size={16} />} size="large" onClick={() => setShowModal(true)}>
            新建 Key
          </Button>
        </div>

        {newKey && (
          <div style={{ marginBottom: 24, padding: 16, background: '#dcfce7', border: '1px solid #bbf7d0', borderRadius: 12, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <div>
              <div style={{ fontSize: 14, fontWeight: 600, color: '#166534' }}>新 API Key 已创建</div>
              <div style={{ fontSize: 13, fontFamily: 'monospace', color: '#15803d', marginTop: 4 }}>{newKey}</div>
              <div style={{ fontSize: 12, color: '#16a34a', marginTop: 4 }}>请立即复制此密钥，关闭后将无法再次查看。</div>
            </div>
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <Button type="primary" style={{ background: '#16a34a', borderColor: '#16a34a' }} icon={copiedId === '__new__' ? <Check size={14} /> : <Copy size={14} />} onClick={() => handleCopy(newKey, '__new__')}>
                {copiedId === '__new__' ? '已复制' : '复制'}
              </Button>
              <button onClick={() => setNewKey(null)} style={{ background: 'none', border: 'none', cursor: 'pointer', padding: 4 }}>
                <X size={16} color="#16a34a" />
              </button>
            </div>
          </div>
        )}

        <div style={{ border: '1px solid var(--border-color)', borderRadius: 12, overflow: 'hidden', marginBottom: 24 }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', textAlign: 'left' }}>
            <thead>
              <tr>
                {['名称', 'API Key', '速率限制', '允许模型', '操作'].map((h) => (
                  <th key={h} style={{ background: '#fafafa', padding: '12px 20px', fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', borderBottom: '1px solid var(--border-color)' }}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {apiKeys.length === 0 ? (
                <tr>
                  <td colSpan={5} style={{ padding: '32px 20px', textAlign: 'center', color: 'var(--text-muted)', fontSize: 14 }}>
                    暂无 API 密钥，所有请求目前无需认证。
                  </td>
                </tr>
              ) : (
                apiKeys.map((k) => (
                  <tr key={k.id}>
                    <td style={{ padding: '16px 20px', fontSize: 14, borderBottom: '1px solid var(--border-color)', fontWeight: 600 }}>{k.name}</td>
                    <td style={{ padding: '16px 20px', fontSize: 14, borderBottom: '1px solid var(--border-color)' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        <code style={{ fontSize: 13, fontFamily: 'monospace', background: '#f1f5f9', padding: '4px 8px', borderRadius: 4 }}>{k.key_masked}</code>
                        <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>仅显示掩码</span>
                      </div>
                    </td>
                    <td style={{ padding: '16px 20px', fontSize: 14, borderBottom: '1px solid var(--border-color)' }}>{k.rate_limit} RPM</td>
                    <td style={{ padding: '16px 20px', fontSize: 14, borderBottom: '1px solid var(--border-color)' }}>
                      <span style={{ padding: '2px 8px', borderRadius: 12, fontSize: 12, fontWeight: 500, border: '1px solid var(--border-color)', color: 'var(--text-muted)' }}>
                        {k.allowed_models.includes('*') ? '* (ALL)' : k.allowed_models.slice(0, 2).join(', ') + (k.allowed_models.length > 2 ? ` +${k.allowed_models.length - 2}` : '')}
                      </span>
                    </td>
                    <td style={{ padding: '16px 20px', fontSize: 14, borderBottom: '1px solid var(--border-color)' }}>
                      <button onClick={() => handleDelete(k.id, k.name)} style={{ background: 'none', border: 'none', cursor: 'pointer', padding: 4 }}>
                        <MoreHorizontal size={16} color="var(--text-muted)" />
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>

        <div style={{ background: '#f8fafc', border: '1px dashed #cbd5e1', borderRadius: 8, padding: 16 }}>
          <div style={{ fontSize: 14, fontWeight: 600, marginBottom: 12 }}>接口连接信息</div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
            <div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 6 }}>Base URL</div>
              <div style={{ background: '#f1f5f9', padding: 10, borderRadius: 4, fontFamily: 'monospace', fontSize: 13, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                {getApiBase()}/v1
                <button onClick={() => handleCopy(`${getApiBase()}/v1`, '__url__')} style={{ background: 'none', border: 'none', cursor: 'pointer' }}>
                  {copiedId === '__url__' ? <Check size={14} color="#16a34a" /> : <Copy size={14} color="var(--text-muted)" />}
                </button>
              </div>
            </div>
            <div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 6 }}>Example (curl)</div>
              <div style={{ background: '#f1f5f9', padding: 10, borderRadius: 4, fontFamily: 'monospace', fontSize: 11, lineHeight: 1.5, wordBreak: 'break-all' }}>
                curl {getApiBase()}/v1/chat/completions -H "Authorization: Bearer sk-xxx" -d '{"{"}model":"...","messages":[...]{"}"}' 
              </div>
            </div>
          </div>
        </div>

        <CreateKeyModal open={showModal} onClose={() => setShowModal(false)} onCreated={async (key) => { setNewKey(key); await loadKeys(); }} />
      </div>
    </div>
  );
}

function CreateKeyModal({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (key: string) => Promise<void> }) {
  const models = useAppStore((s) => s.models);
  const loadModels = useAppStore((s) => s.loadModels);
  const [form] = Form.useForm();
  const [allModels, setAllModels] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const { message } = AntApp.useApp();

  useEffect(() => { loadModels(); }, [loadModels]);

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      setSubmitting(true);
      const res = await api.createKey({
        name: values.name,
        rate_limit: values.rate_limit,
        allowed_models: allModels ? ['*'] : (values.allowed_models || []),
      });
      message.success('API Key 创建成功');
      await onCreated(res.key);
      form.resetFields();
      setAllModels(true);
      onClose();
    } catch (e: unknown) {
      if (e instanceof Error) message.error(e.message);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal
      title="新建 API Key"
      open={open}
      onCancel={onClose}
      onOk={handleSubmit}
      confirmLoading={submitting}
      okText="创建密钥"
      cancelText="取消"
      width={520}
      destroyOnClose
    >
      <Form form={form} layout="vertical" style={{ marginTop: 16 }} initialValues={{ rate_limit: 60 }}>
        <Form.Item label="密钥名称" name="name" rules={[{ required: true, message: '请填写密钥名称' }]}>
          <Input placeholder="例如 Production-Web" />
        </Form.Item>

        <Form.Item label="速率限制 (RPM)" name="rate_limit">
          <InputNumber min={1} max={100000} style={{ width: 180 }} />
        </Form.Item>

        <Form.Item label="允许模型">
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: allModels ? 0 : 12 }}>
            <Switch checked={allModels} onChange={setAllModels} size="small" />
            <span style={{ fontSize: 14 }}>全部模型</span>
          </div>
          {!allModels && (
            <Form.Item name="allowed_models" noStyle>
              <Select
                mode="multiple"
                placeholder="选择允许的模型"
                style={{ width: '100%' }}
                options={models.map((m) => ({ value: m, label: m }))}
                maxTagCount="responsive"
              />
            </Form.Item>
          )}
        </Form.Item>
      </Form>
    </Modal>
  );
}
