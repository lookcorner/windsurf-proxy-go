import { useEffect, useState } from 'react';
import { Save, Download, Upload, AlertTriangle } from 'lucide-react';
import { Select, InputNumber, Button, Switch, App as AntApp } from 'antd';
import { useAppStore } from '@/stores/appStore';
import * as api from '@/lib/api';

export default function Settings() {
  const config = useAppStore((s) => s.config);
  const loadConfig = useAppStore((s) => s.loadConfig);
  const { message, modal } = AntApp.useApp();

  const [port, setPort] = useState(8000);
  const [host, setHost] = useState('127.0.0.1');
  const [logLevel, setLogLevel] = useState('info');
  const [strategy, setStrategy] = useState('weighted_round_robin');
  const [healthInterval, setHealthInterval] = useState(30);
  const [maxRetries, setMaxRetries] = useState(3);
  const [retryDelay, setRetryDelay] = useState(1.0);
  const [auditLog, setAuditLog] = useState(false);
  const [saving, setSaving] = useState(false);

  useEffect(() => { loadConfig(); }, [loadConfig]);

  useEffect(() => {
    if (config) {
      setPort(config.server.port);
      setHost(config.server.host);
      setLogLevel(config.server.log_level);
      setStrategy(config.balancing.strategy);
      setHealthInterval(config.balancing.health_check_interval);
      setMaxRetries(config.balancing.max_retries);
      setRetryDelay(config.balancing.retry_delay);
      setAuditLog(config.logging?.audit ?? false);
    }
  }, [config]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await api.updateConfig({
        server: { host, port, log_level: logLevel },
        balancing: { strategy, health_check_interval: healthInterval, max_retries: maxRetries, retry_delay: retryDelay },
        logging: { audit: auditLog },
      });
      message.success('设置已保存');
      await loadConfig();
    } catch (e) { console.error(e); message.error('保存失败'); }
    finally { setSaving(false); }
  };

  const handleReset = () => {
    modal.confirm({
      title: '恢复出厂设置',
      content: '确认清空所有配置并恢复到默认状态？该操作不可逆。',
      okText: '确认重置',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await api.updateConfig({
            server: { host: '127.0.0.1', port: 8000, log_level: 'info' },
            balancing: { strategy: 'round_robin', health_check_interval: 30, max_retries: 3, retry_delay: 1.0 },
          });
          message.success('已恢复出厂设置');
          await loadConfig();
        } catch (e) { console.error(e); message.error('重置失败'); }
      },
    });
  };

  const handleExport = async () => {
    try {
      const res = await fetch(`${api.getApiBase()}/api/config`);
      const data = await res.json();
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a'); a.href = url; a.download = 'windsurf-proxy-config.json'; a.click();
      URL.revokeObjectURL(url);
      message.success('配置已导出');
    } catch (e) { console.error(e); message.error('导出失败'); }
  };

  const handleImport = () => {
    const input = document.createElement('input');
    input.type = 'file'; input.accept = '.json';
    input.onchange = async (e) => {
      const file = (e.target as HTMLInputElement).files?.[0];
      if (!file) return;
      try {
        const text = await file.text();
        const data = JSON.parse(text);
        await api.updateConfig(data);
        message.success('配置已导入');
        await loadConfig();
      } catch (err) { console.error(err); message.error('导入失败：仅支持 JSON 配置文件'); }
    };
    input.click();
  };

  const sectionStyle: React.CSSProperties = { marginBottom: 32, background: 'white', border: '1px solid var(--border-color)', borderRadius: 12, overflow: 'hidden', boxShadow: '0 2px 8px rgba(0,0,0,0.02)' };
  const sectionHeader: React.CSSProperties = { padding: '16px 20px', background: '#fafafa', borderBottom: '1px solid var(--border-color)', fontWeight: 600, fontSize: 14, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em' };
  const itemStyle: React.CSSProperties = { display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: 20, borderBottom: '1px solid var(--border-color)' };

  return (
    <div className="page-enter" style={{ flex: 1, overflowY: 'auto', background: '#ffffff' }}>
      <div style={{ padding: 40, maxWidth: 1100, margin: '0 auto', paddingBottom: 80 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 32 }}>
          <h1 style={{ fontSize: 24, fontWeight: 600 }}>系统设置</h1>
          <Button type="primary" icon={<Save size={16} />} size="large" loading={saving} onClick={handleSave}>
            保存修改
          </Button>
        </div>

        {/* Network */}
        <div style={sectionStyle}>
          <div style={sectionHeader}>网络与代理配置</div>
          <div style={itemStyle}>
            <div>
              <h4 style={{ fontSize: 15, fontWeight: 500, marginBottom: 4 }}>代理服务监听端口</h4>
              <p style={{ fontSize: 13, color: 'var(--text-muted)', maxWidth: 400, lineHeight: 1.5 }}>Windsurf Proxy 对外提供服务的本地端口号。</p>
            </div>
            <InputNumber value={port} onChange={(v) => setPort(v ?? 8000)} min={1} max={65535} style={{ width: 120 }} />
          </div>
          <div style={itemStyle}>
            <div>
              <h4 style={{ fontSize: 15, fontWeight: 500, marginBottom: 4 }}>绑定地址 (Bind Address)</h4>
              <p style={{ fontSize: 13, color: 'var(--text-muted)', maxWidth: 400, lineHeight: 1.5 }}>限制允许访问代理服务的来源。127.0.0.1 仅限本机，0.0.0.0 允许局域网访问。</p>
            </div>
            <Select value={host} onChange={setHost} style={{ width: 200 }} options={[
              { value: '127.0.0.1', label: '127.0.0.1 (本地)' },
              { value: '0.0.0.0', label: '0.0.0.0 (所有接口)' },
            ]} />
          </div>
        </div>

        {/* Load Balancing */}
        <div style={sectionStyle}>
          <div style={sectionHeader}>负载均衡</div>
          <div style={itemStyle}>
            <div>
              <h4 style={{ fontSize: 15, fontWeight: 500, marginBottom: 4 }}>负载均衡策略</h4>
              <p style={{ fontSize: 13, color: 'var(--text-muted)', maxWidth: 400, lineHeight: 1.5 }}>选择多实例间请求的分发方式。</p>
            </div>
            <Select value={strategy} onChange={setStrategy} style={{ width: 220 }} options={[
              { value: 'round_robin', label: 'Round Robin' },
              { value: 'weighted_round_robin', label: 'Weighted Round Robin' },
              { value: 'least_connections', label: 'Least Connections' },
            ]} />
          </div>
          <div style={itemStyle}>
            <div>
              <h4 style={{ fontSize: 15, fontWeight: 500, marginBottom: 4 }}>健康检查间隔 (秒)</h4>
              <p style={{ fontSize: 13, color: 'var(--text-muted)', maxWidth: 400, lineHeight: 1.5 }}>自动检测实例是否存活的时间间隔。</p>
            </div>
            <InputNumber value={healthInterval} onChange={(v) => setHealthInterval(v ?? 30)} min={5} max={600} style={{ width: 120 }} />
          </div>
          <div style={itemStyle}>
            <div>
              <h4 style={{ fontSize: 15, fontWeight: 500, marginBottom: 4 }}>最大重试次数</h4>
              <p style={{ fontSize: 13, color: 'var(--text-muted)', maxWidth: 400, lineHeight: 1.5 }}>请求失败后尝试其他实例的最大次数。</p>
            </div>
            <InputNumber value={maxRetries} onChange={(v) => setMaxRetries(v ?? 3)} min={0} max={10} style={{ width: 120 }} />
          </div>
          <div style={{ ...itemStyle, borderBottom: 'none' }}>
            <div>
              <h4 style={{ fontSize: 15, fontWeight: 500, marginBottom: 4 }}>重试延迟 (秒)</h4>
              <p style={{ fontSize: 13, color: 'var(--text-muted)', maxWidth: 400, lineHeight: 1.5 }}>每次重试之间的等待时间。</p>
            </div>
            <InputNumber value={retryDelay} onChange={(v) => setRetryDelay(v ?? 1.0)} min={0} max={30} step={0.1} style={{ width: 120 }} />
          </div>
        </div>

        {/* Advanced */}
        <div style={sectionStyle}>
          <div style={sectionHeader}>高级选项</div>
          <div style={itemStyle}>
            <div>
              <h4 style={{ fontSize: 15, fontWeight: 500, marginBottom: 4 }}>请求审计日志</h4>
              <p style={{ fontSize: 13, color: 'var(--text-muted)', maxWidth: 480, lineHeight: 1.5 }}>
                打开后，每次请求的入参（含 messages 完整内容）、上游 windsurf 地址、返回内容、耗时会写入
                <code style={{ margin: '0 4px' }}>requests-YYYYMMDD.jsonl</code>
                （位于日志目录）。包含可能敏感的对话内容，默认关闭。
              </p>
            </div>
            <Switch checked={auditLog} onChange={setAuditLog} />
          </div>
          <div style={itemStyle}>
            <div>
              <h4 style={{ fontSize: 15, fontWeight: 500, marginBottom: 4 }}>日志级别</h4>
              <p style={{ fontSize: 13, color: 'var(--text-muted)', maxWidth: 400, lineHeight: 1.5 }}>控制终端和日志文件中输出信息的详细程度。</p>
            </div>
            <Select value={logLevel} onChange={setLogLevel} style={{ width: 160 }} options={[
              { value: 'debug', label: 'Debug' },
              { value: 'info', label: 'Info' },
              { value: 'warning', label: 'Warning' },
              { value: 'error', label: 'Error' },
            ]} />
          </div>
          <div style={itemStyle}>
            <div>
              <h4 style={{ fontSize: 15, fontWeight: 500, marginBottom: 4 }}>数据备份与恢复</h4>
              <p style={{ fontSize: 13, color: 'var(--text-muted)', maxWidth: 400, lineHeight: 1.5 }}>导出所有配置、实例和 API 密钥。</p>
            </div>
            <div style={{ display: 'flex', gap: 8 }}>
              <Button icon={<Download size={14} />} onClick={handleExport}>导出</Button>
              <Button icon={<Upload size={14} />} onClick={handleImport}>导入</Button>
            </div>
          </div>
          <div style={{ ...itemStyle, borderBottom: 'none' }}>
            <div>
              <h4 style={{ fontSize: 15, fontWeight: 500, marginBottom: 4, color: 'var(--danger)' }}>重置所有设置</h4>
              <p style={{ fontSize: 13, color: 'var(--text-muted)', maxWidth: 400, lineHeight: 1.5 }}>清空所有配置并恢复到默认状态，该操作不可逆。</p>
            </div>
            <Button danger icon={<AlertTriangle size={14} />} onClick={handleReset}>恢复出厂设置</Button>
          </div>
        </div>
      </div>
    </div>
  );
}
