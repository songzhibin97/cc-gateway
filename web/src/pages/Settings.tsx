import React, { useEffect, useMemo, useState } from 'react';
import type { AxiosError } from 'axios';
import { Badge, Button, Card, Descriptions, Space, Typography, message } from 'antd';
import { CopyOutlined, ReloadOutlined } from '@ant-design/icons';
import { healthCheck, triggerCleanup } from '../api/stats';

const getErrorMessage = (error: unknown) => {
  const axiosError = error as AxiosError<{ message?: string; error?: string }>;
  return axiosError.response?.data?.message ?? axiosError.response?.data?.error ?? axiosError.message ?? '请求失败';
};

const Settings: React.FC = () => {
  const [messageApi, contextHolder] = message.useMessage();
  const [healthStatus, setHealthStatus] = useState<'healthy' | 'unhealthy' | 'loading'>('loading');
  const [checkingHealth, setCheckingHealth] = useState(false);
  const [cleaning, setCleaning] = useState(false);

  const gatewayOrigin = useMemo(
    () => `${window.location.protocol}//${window.location.hostname}:19999`,
    [],
  );

  const adminOrigin = useMemo(
    () => import.meta.env.VITE_ADMIN_API_URL || window.location.origin,
    [],
  );

  const cliSnippet = useMemo(
    () => `export ANTHROPIC_BASE_URL=${window.location.protocol}//${window.location.hostname}:19999
export ANTHROPIC_API_KEY=<your-api-key>`,
    [],
  );

  const loadHealth = async () => {
    setCheckingHealth(true);
    try {
      const response = await healthCheck();
      setHealthStatus(response.data.status === 'ok' ? 'healthy' : 'unhealthy');
    } catch {
      setHealthStatus('unhealthy');
    } finally {
      setCheckingHealth(false);
    }
  };

  useEffect(() => {
    void loadHealth();
  }, []);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(cliSnippet);
      messageApi.success('配置已复制');
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    }
  };

  const handleCleanup = async () => {
    setCleaning(true);
    try {
      const response = await triggerCleanup();
      messageApi.success(`清理完成，删除 ${response.data.deleted} 条 Payload`);
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    } finally {
      setCleaning(false);
    }
  };

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      {contextHolder}
      <Typography.Title level={2} style={{ margin: 0 }}>
        系统设置
      </Typography.Title>
      <Card title="连接信息" extra={<Button icon={<ReloadOutlined />} loading={checkingHealth} onClick={() => void loadHealth()}>刷新状态</Button>}>
        <Descriptions column={1} bordered>
          <Descriptions.Item label="Gateway API">{gatewayOrigin}</Descriptions.Item>
          <Descriptions.Item label="Admin API">{adminOrigin}</Descriptions.Item>
          <Descriptions.Item label="系统状态">
            <Badge
              status={healthStatus === 'healthy' ? 'success' : healthStatus === 'loading' ? 'processing' : 'error'}
              text={healthStatus === 'healthy' ? '正常' : healthStatus === 'loading' ? '检查中' : '异常'}
            />
          </Descriptions.Item>
        </Descriptions>
      </Card>
      <Card
        title="Payload 清理"
        extra={
          <Button type="primary" loading={cleaning} onClick={() => void handleCleanup()}>
            立即清理
          </Button>
        }
      >
        <Space direction="vertical" size={8}>
          <Typography.Text>Retention: 7 天</Typography.Text>
          <Typography.Text type="secondary">用于清理已过期的请求与响应 Payload 存档。</Typography.Text>
        </Space>
      </Card>
      <Card
        title="CLI Configuration Guide"
        extra={
          <Button icon={<CopyOutlined />} onClick={() => void handleCopy()}>
            复制
          </Button>
        }
      >
        <pre style={{ margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{cliSnippet}</pre>
      </Card>
      <Card title="Pricing Info">
        <Typography.Text>价格配置在 config.yaml 中管理，发送 SIGHUP 热重载</Typography.Text>
      </Card>
    </Space>
  );
};

export default Settings;
