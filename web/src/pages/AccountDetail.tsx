import React, { useEffect, useMemo, useState } from 'react';
import type { AxiosError } from 'axios';
import {
  Button,
  Card,
  Col,
  Descriptions,
  Row,
  Space,
  Spin,
  Table,
  Tabs,
  Typography,
  message,
} from 'antd';
import type { TableProps } from 'antd';
import { ArrowLeftOutlined } from '@ant-design/icons';
import { useNavigate, useParams } from 'react-router-dom';
import { getAccount } from '../api/accounts';
import { queryLogs } from '../api/logs';
import { providerLabelMap, renderAccountStatus, renderBreakerState } from '../components/accountUi';
import type { Account, RequestLog } from '../types';

const getErrorMessage = (error: unknown) => {
  const axiosError = error as AxiosError<{ message?: string; error?: string }>;
  return axiosError.response?.data?.message ?? axiosError.response?.data?.error ?? axiosError.message ?? '请求失败';
};

const renderValue = (value: unknown) => {
  if (value === null || value === undefined || value === '') {
    return '-';
  }

  if (Array.isArray(value)) {
    return value.length > 0 ? value.join(', ') : '-';
  }

  if (typeof value === 'object') {
    return (
      <Typography.Text style={{ whiteSpace: 'pre-wrap' }}>
        {JSON.stringify(value, null, 2)}
      </Typography.Text>
    );
  }

  return String(value);
};

const renderCircuitBreakerConfig = (config: Account['circuit_breaker']) => (
  <Space direction="vertical" size={4}>
    <Typography.Text>术语：closed=正常放行，open=已熔断拦截，half_open=试探恢复</Typography.Text>
    <Typography.Text>触发条件：连续失败 {config.failure_threshold} 次后进入 open</Typography.Text>
    <Typography.Text>熔断时长：进入 open 后保持 {config.open_duration}</Typography.Text>
    <Typography.Text>恢复条件：half_open 下连续成功 {config.success_threshold} 次后回到 closed</Typography.Text>
  </Space>
);

const AccountDetail: React.FC = () => {
  const navigate = useNavigate();
  const { id } = useParams<{ id: string }>();
  const [messageApi, contextHolder] = message.useMessage();
  const [loading, setLoading] = useState(true);
  const [account, setAccount] = useState<Account | null>(null);
  const [logs, setLogs] = useState<RequestLog[]>([]);

  useEffect(() => {
    const loadDetail = async () => {
      if (!id) {
        messageApi.error('缺少账号 ID');
        setLoading(false);
        return;
      }

      setLoading(true);
      try {
        const [accountResponse, logsResponse] = await Promise.all([
          getAccount(id),
          queryLogs({ account_id: id, limit: 50 }),
        ]);
        setAccount(accountResponse.data);
        setLogs(logsResponse.data);
      } catch (error) {
        messageApi.error(getErrorMessage(error));
      } finally {
        setLoading(false);
      }
    };

    void loadDetail();
  }, [id, messageApi]);

  const logColumns = useMemo<TableProps<RequestLog>['columns']>(
    () => [
      {
        title: '时间',
        dataIndex: 'CreatedAt',
        key: 'CreatedAt',
        render: (value: string) => new Date(value).toLocaleString(),
      },
      {
        title: '模型',
        dataIndex: 'ModelRequested',
        key: 'ModelRequested',
      },
      {
        title: '实际模型',
        dataIndex: 'ModelActual',
        key: 'ModelActual',
      },
      {
        title: '状态码',
        dataIndex: 'StatusCode',
        key: 'StatusCode',
      },
      {
        title: '耗时(ms)',
        dataIndex: 'LatencyMs',
        key: 'LatencyMs',
      },
      {
        title: '费用($)',
        dataIndex: 'CostUSD',
        key: 'CostUSD',
        render: (value: number) => value.toFixed(6),
      },
      {
        title: '错误',
        dataIndex: 'Error',
        key: 'Error',
        render: (value: string) => value || '-',
      },
    ],
    [],
  );

  const basicInfo = account
    ? [
        { key: 'id', label: 'ID', children: renderValue(account.id) },
        { key: 'name', label: '名称', children: renderValue(account.name) },
        { key: 'provider', label: 'Provider', children: providerLabelMap[account.provider] ?? account.provider },
        { key: 'status', label: '状态', children: renderAccountStatus(account.status) },
        { key: 'api_key', label: 'API Key', children: renderValue(account.api_key) },
        { key: 'base_url', label: 'Base URL', children: renderValue(account.base_url) },
        { key: 'proxy_url', label: 'Proxy URL', children: renderValue(account.proxy_url) },
        { key: 'user_agent', label: 'User-Agent', children: renderValue(account.user_agent) },
        { key: 'allowed_models', label: '允许模型', children: renderValue(account.allowed_models) },
        {
          key: 'model_aliases',
          label: '模型映射',
          children: renderValue(account.model_aliases),
        },
        { key: 'max_concurrent', label: '最大并发', children: renderValue(account.max_concurrent) },
        {
          key: 'breaker_state',
          label: '熔断状态',
          children: renderBreakerState(account.breaker_state),
        },
        {
          key: 'circuit_breaker',
          label: '熔断配置',
          children: renderCircuitBreakerConfig(account.circuit_breaker),
        },
        { key: 'extra', label: 'Extra', children: renderValue(account.extra) },
      ]
    : [];

  return (
    <Space direction="vertical" size={24} style={{ width: '100%' }}>
      {contextHolder}

      <Space align="center">
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/accounts')}>
          返回
        </Button>
        <Typography.Title level={2} style={{ margin: 0 }}>
          {account ? `${account.id} / ${account.name}` : `账号详情 ${id ?? ''}`}
        </Typography.Title>
      </Space>

      <Spin spinning={loading}>
        {account && (
          <Space direction="vertical" size={24} style={{ width: '100%' }}>
            <Row gutter={[16, 16]}>
              <Col xs={24} md={8}>
                <Card title="状态">{renderAccountStatus(account.status)}</Card>
              </Col>
              <Col xs={24} md={8}>
                <Card title="熔断器">{renderBreakerState(account.breaker_state)}</Card>
              </Col>
              <Col xs={24} md={8}>
                <Card title="最大并发">{account.max_concurrent}</Card>
              </Col>
            </Row>

            <Tabs
              items={[
                {
                  key: 'basic',
                  label: '基本信息',
                  children: (
                    <Card>
                      <Descriptions column={1} bordered items={basicInfo} />
                    </Card>
                  ),
                },
                {
                  key: 'logs',
                  label: '请求日志',
                  children: (
                    <Card>
                      <Table<RequestLog>
                        columns={logColumns}
                        dataSource={logs}
                        rowKey="ID"
                        pagination={{ pageSize: 10, showSizeChanger: false }}
                        scroll={{ x: 960 }}
                      />
                    </Card>
                  ),
                },
              ]}
            />
          </Space>
        )}
      </Spin>
    </Space>
  );
};

export default AccountDetail;
