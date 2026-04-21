import { useEffect, useState } from 'react';
import { ArrowLeft, Copy } from 'lucide-react';
import { Button, Input, Tag, App as AntApp } from 'antd';
import { useAppStore } from '@/stores/appStore';

const MODEL_FAMILIES: Record<string, { color: string; prefix: string[] }> = {
  Claude: { color: 'purple', prefix: ['claude-'] },
  GPT: { color: 'green', prefix: ['gpt-'] },
  'O-Series': { color: 'blue', prefix: ['o3', 'o4'] },
  Gemini: { color: 'cyan', prefix: ['gemini-'] },
  DeepSeek: { color: 'geekblue', prefix: ['deepseek-'] },
  Llama: { color: 'orange', prefix: ['llama-'] },
  Qwen: { color: 'volcano', prefix: ['qwen-'] },
  Grok: { color: 'red', prefix: ['grok-'] },
  Kimi: { color: 'magenta', prefix: ['kimi-'] },
  GLM: { color: 'lime', prefix: ['glm-'] },
  Other: { color: 'default', prefix: [] },
};

function getFamily(model: string): string {
  for (const [name, { prefix }] of Object.entries(MODEL_FAMILIES)) {
    if (name === 'Other') continue;
    if (prefix.some((p) => model.startsWith(p))) return name;
  }
  return 'Other';
}

export default function ModelsView() {
  const models = useAppStore((s) => s.models);
  const loadModels = useAppStore((s) => s.loadModels);
  const setPage = useAppStore((s) => s.setPage);
  const [search, setSearch] = useState('');
  const { message } = AntApp.useApp();

  const handleCopy = (name: string) => {
    navigator.clipboard.writeText(name).then(() => {
      message.success(`已复制: ${name}`);
    });
  };

  useEffect(() => { loadModels(); }, [loadModels]);

  const filtered = search
    ? models.filter((m) => m.toLowerCase().includes(search.toLowerCase()))
    : models;

  // Group by family
  const grouped: Record<string, string[]> = {};
  filtered.forEach((m) => {
    const family = getFamily(m);
    if (!grouped[family]) grouped[family] = [];
    grouped[family].push(m);
  });

  // Sort families
  const familyOrder = Object.keys(MODEL_FAMILIES);
  const sortedFamilies = Object.entries(grouped).sort(
    (a, b) => familyOrder.indexOf(a[0]) - familyOrder.indexOf(b[0])
  );

  return (
    <div className="page-enter" style={{ flex: 1, overflowY: 'auto', background: '#ffffff' }}>
      <div style={{ padding: 40, maxWidth: 1100, margin: '0 auto', paddingBottom: 80 }}>
        {/* Header */}
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 32 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
            <Button icon={<ArrowLeft size={16} />} onClick={() => setPage('dashboard')} />
            <h1 style={{ fontSize: 24, fontWeight: 600 }}>已注册模型</h1>
            <Tag style={{ fontSize: 14, padding: '2px 12px' }}>{models.length} 个</Tag>
          </div>
          <Input.Search
            placeholder="搜索模型..."
            allowClear
            style={{ width: 280 }}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>

        {/* Model groups */}
        {sortedFamilies.map(([family, familyModels]) => {
          const familyConfig = MODEL_FAMILIES[family] || MODEL_FAMILIES.Other;
          return (
            <div key={family} style={{ marginBottom: 28 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12 }}>
                <span style={{ fontSize: 16, fontWeight: 600 }}>{family}</span>
                <Tag color={familyConfig.color}>{familyModels.length}</Tag>
              </div>
              <div style={{
                display: 'grid',
                gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))',
                gap: 10,
              }}>
                {familyModels.map((m) => (
                  <div
                    key={m}
                    onClick={() => handleCopy(m)}
                    style={{
                      padding: '10px 14px',
                      border: '1px solid var(--border-color)',
                      borderRadius: 8,
                      fontSize: 13,
                      fontFamily: 'monospace',
                      background: '#fafafa',
                      transition: 'all 0.15s',
                      cursor: 'pointer',
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'space-between',
                      gap: 8,
                    }}
                    onMouseEnter={(e) => {
                      e.currentTarget.style.borderColor = '#4f46e5';
                      e.currentTarget.style.background = '#f0f0ff';
                    }}
                    onMouseLeave={(e) => {
                      e.currentTarget.style.borderColor = 'var(--border-color)';
                      e.currentTarget.style.background = '#fafafa';
                    }}
                  >
                    <span>{m}</span>
                    <Copy size={12} style={{ color: 'var(--text-muted)', flexShrink: 0 }} />
                  </div>
                ))}
              </div>
            </div>
          );
        })}

        {filtered.length === 0 && (
          <div style={{ textAlign: 'center', padding: '60px 0', color: 'var(--text-muted)', fontSize: 14 }}>
            未找到匹配的模型
          </div>
        )}
      </div>
    </div>
  );
}
