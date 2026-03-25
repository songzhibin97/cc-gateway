import { Badge, Tag } from 'antd';
import type { Account } from '../types';

export const providerOptions: Array<{ label: string; value: Account['provider'] }> = [
  { label: 'OpenAI', value: 'openai' },
  { label: 'Gemini', value: 'gemini' },
  { label: 'Anthropic', value: 'anthropic' },
  { label: 'Custom OpenAI', value: 'custom_openai' },
  { label: 'Custom Anthropic', value: 'custom_anthropic' },
];

export const providerLabelMap: Record<Account['provider'], string> = {
  openai: 'OpenAI',
  gemini: 'Gemini',
  anthropic: 'Anthropic',
  custom_openai: 'Custom OpenAI',
  custom_anthropic: 'Custom Anthropic',
};

export const providerAlertMap: Record<Account['provider'], { message: string; description: string }> = {
  openai: {
    message: 'OpenAI 配置提示',
    description: 'Base URL 留空时使用 OpenAI 官方地址。reasoning 配置只会对支持推理的模型生效。',
  },
  gemini: {
    message: 'Gemini 配置提示',
    description: 'thinking_enabled 和 thinking_budget 会写入 extra。safety_settings 会按 4 个风险类别一起提交。',
  },
  anthropic: {
    message: 'Anthropic 配置提示',
    description: '网关会透传 anthropic-version 和 anthropic-beta 等请求头，扩展 thinking 配置由客户端控制。',
  },
  custom_openai: {
    message: 'Custom OpenAI 配置提示',
    description: '必须填写 Base URL，接口兼容 OpenAI，reasoning 配置与 OpenAI 一致。',
  },
  custom_anthropic: {
    message: 'Custom Anthropic 配置提示',
    description: '必须填写 Base URL，接口兼容 Anthropic，请确认上游支持透传相关 Header。',
  },
};

export const renderAccountStatus = (status: Account['status']) => (
  <Tag color={status === 'enabled' ? 'green' : 'red'}>{status === 'enabled' ? '启用' : '禁用'}</Tag>
);

export const renderBreakerState = (state?: string) => {
  const normalized = state ?? 'unknown';
  const config =
    normalized === 'closed'
      ? { status: 'success' as const, text: 'closed' }
      : normalized === 'half_open'
        ? { status: 'warning' as const, text: 'half_open' }
        : normalized === 'open'
          ? { status: 'error' as const, text: 'open' }
          : { status: 'default' as const, text: normalized };

  return <Badge status={config.status} text={config.text} />;
};
