import React, { useEffect, useMemo, useState } from 'react';
import type { AxiosError } from 'axios';
import { Card, Col, Row, Space, Spin, Statistic, Table, Tooltip, Typography, message } from 'antd';
import type { TableProps } from 'antd';
import { useNavigate } from 'react-router-dom';
import { listAccounts } from '../api/accounts';
import { queryLogs } from '../api/logs';
import { queryCostStats } from '../api/stats';
import { providerLabelMap, renderAccountStatus, renderBreakerState } from '../components/accountUi';
import type { Account } from '../types';

const getErrorMessage = (error: unknown) => {
  const axiosError = error as AxiosError<{ message?: string; error?: string }>;
  return axiosError.response?.data?.message ?? axiosError.response?.data?.error ?? axiosError.message ?? '请求失败';
};

const getTodayRange = () => {
  const start = new Date();
  start.setHours(0, 0, 0, 0);
  const end = new Date(start);
  end.setDate(end.getDate() + 1);
  return {
    from: start.toISOString(),
    to: end.toISOString(),
  };
};

const Dashboard: React.FC = () => {
  const navigate = useNavigate();
  const [messageApi, contextHolder] = message.useMessage();
  const [loading, setLoading] = useState(true);
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [todayRequests, setTodayRequests] = useState(0);
  const [todayCost, setTodayCost] = useState(0);

  useEffect(() => {
    const loadDashboard = async () => {
      setLoading(true);
      try {
        const todayRange = getTodayRange();
        const [accountsResponse, logsResponse, costResponse] = await Promise.all([
          listAccounts(),
          queryLogs({ from: todayRange.from, to: todayRange.to, limit: 1000 }),
          queryCostStats({ from: todayRange.from, to: todayRange.to }),
        ]);

        setAccounts(accountsResponse.data);
        setTodayRequests(logsResponse.data.length);
        setTodayCost(costResponse.data.reduce((sum, item) => sum + item.total_cost_usd, 0));
      } catch (error) {
        messageApi.error(getErrorMessage(error));
      } finally {
        setLoading(false);
      }
    };

    void loadDashboard();
  }, [messageApi]);

  const columns = useMemo<TableProps<Account>['columns']>(
    () => [
      {
        title: 'ID',
        dataIndex: 'id',
        key: 'id',
      },
      {
        title: '名称',
        dataIndex: 'name',
        key: 'name',
      },
      {
        title: 'Provider',
        dataIndex: 'provider',
        key: 'provider',
        render: (provider: Account['provider']) => providerLabelMap[provider] ?? provider,
      },
      {
        title: '状态',
        dataIndex: 'status',
        key: 'status',
        render: renderAccountStatus,
      },
      {
        title: <Tooltip title="closed=正常放行，open=已熔断拦截，half_open=试探恢复">熔断状态</Tooltip>,
        dataIndex: 'breaker_state',
        key: 'breaker_state',
        render: renderBreakerState,
      },
      {
        title: '最大并发',
        dataIndex: 'max_concurrent',
        key: 'max_concurrent',
      },
    ],
    [],
  );

  return (
    <Space direction="vertical" size={24} style={{ width: '100%' }}>
      {contextHolder}
      <Typography.Title level={2} style={{ margin: 0 }}>
        概览
      </Typography.Title>

      <Spin spinning={loading}>
        <Space direction="vertical" size={24} style={{ width: '100%' }}>
          <Row gutter={[16, 16]}>
            <Col xs={24} sm={12} lg={6}>
              <Card>
                <Statistic title="源账号总数" value={accounts.length} />
              </Card>
            </Col>
            <Col xs={24} sm={12} lg={6}>
              <Card>
                <Statistic title="活跃请求" value={0} />
              </Card>
            </Col>
            <Col xs={24} sm={12} lg={6}>
              <Card>
                <Statistic title="今日请求" value={todayRequests} />
              </Card>
            </Col>
            <Col xs={24} sm={12} lg={6}>
              <Card>
                <Statistic title="今日费用" value={todayCost} precision={4} prefix="$" />
              </Card>
            </Col>
          </Row>

          <Card title="账号健康状态">
            <Table<Account>
              columns={columns}
              dataSource={accounts}
              loading={loading}
              pagination={false}
              rowKey="id"
              onRow={(record) => ({
                onClick: () => navigate(`/accounts/${record.id}`),
                style: { cursor: 'pointer' },
              })}
              scroll={{ x: 720 }}
            />
          </Card>
        </Space>
      </Spin>
    </Space>
  );
};

export default Dashboard;
