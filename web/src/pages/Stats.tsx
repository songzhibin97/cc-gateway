import React, { useEffect, useMemo, useState } from 'react';
import type { AxiosError } from 'axios';
import dayjs, { type Dayjs } from 'dayjs';
import ReactECharts from 'echarts-for-react';
import {
  Card,
  Col,
  DatePicker,
  Empty,
  Row,
  Select,
  Space,
  Statistic,
  Table,
  Typography,
  message,
} from 'antd';
import type { TableProps } from 'antd';
import { queryCostStats, type StatsFilter } from '../api/stats';
import type { CostStats } from '../types';

type RangePreset = 'today' | 'week' | 'month' | 'last30' | 'custom';
type Dimension = 'account' | 'key' | 'model';

interface TimeRange {
  from: string;
  to: string;
}

const getErrorMessage = (error: unknown) => {
  const axiosError = error as AxiosError<{ message?: string; error?: string }>;
  return axiosError.response?.data?.message ?? axiosError.response?.data?.error ?? axiosError.message ?? '请求失败';
};

const getPresetRange = (preset: Exclude<RangePreset, 'custom'>): [Dayjs, Dayjs] => {
  const now = dayjs();

  switch (preset) {
    case 'today':
      return [now.startOf('day'), now.endOf('day')];
    case 'week': {
      const weekday = now.day();
      const mondayOffset = weekday === 0 ? 6 : weekday - 1;
      return [now.subtract(mondayOffset, 'day').startOf('day'), now.endOf('day')];
    }
    case 'month':
      return [now.startOf('month'), now.endOf('day')];
    case 'last30':
      return [now.subtract(29, 'day').startOf('day'), now.endOf('day')];
  }
};

const toUtcRange = (range: [Dayjs, Dayjs]): TimeRange => ({
  from: range[0].toDate().toISOString(),
  to: range[1].toDate().toISOString(),
});

const Stats: React.FC = () => {
  const [messageApi, contextHolder] = message.useMessage();
  const [stats, setStats] = useState<CostStats[]>([]);
  const [loading, setLoading] = useState(true);
  const [preset, setPreset] = useState<RangePreset>('today');
  const [dimension, setDimension] = useState<Dimension>('account');
  const [customRange, setCustomRange] = useState<[Dayjs, Dayjs] | null>(null);

  const currentRange = useMemo<[Dayjs, Dayjs]>(() => {
    if (preset === 'custom' && customRange) {
      return customRange;
    }
    return getPresetRange(preset === 'custom' ? 'today' : preset);
  }, [customRange, preset]);

  const loadStats = async (nextDimension: Dimension, range: [Dayjs, Dayjs]) => {
    setLoading(true);
    try {
      const utcRange = toUtcRange(range);
      const response = await queryCostStats({
        group_by: nextDimension as StatsFilter['group_by'],
        from: utcRange.from,
        to: utcRange.to,
      });
      setStats(response.data);
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadStats(dimension, currentRange);
  }, [dimension, currentRange]);

  const summary = useMemo(
    () =>
      stats.reduce(
        (acc, item) => ({
          totalCost: acc.totalCost + item.total_cost_usd,
          totalRequests: acc.totalRequests + item.request_count,
          totalInputTokens: acc.totalInputTokens + item.total_input_tokens,
          totalOutputTokens: acc.totalOutputTokens + item.total_output_tokens,
        }),
        {
          totalCost: 0,
          totalRequests: 0,
          totalInputTokens: 0,
          totalOutputTokens: 0,
        },
      ),
    [stats],
  );

  const dimensionLabel = useMemo(() => {
    switch (dimension) {
      case 'account':
        return '账号';
      case 'key':
        return '密钥';
      case 'model':
        return '模型';
    }
  }, [dimension]);

  const chartOption = useMemo(
    () => ({
      tooltip: { trigger: 'axis' },
      legend: { data: ['费用', '请求数'] },
      xAxis: {
        type: 'category',
        data: stats.map((item) => item.group_value || '-'),
        axisLabel: { interval: 0, rotate: stats.length > 6 ? 20 : 0 },
      },
      yAxis: [
        { type: 'value', name: 'USD' },
        { type: 'value', name: '请求数' },
      ],
      series: [
        {
          type: 'bar',
          data: stats.map((item) => item.total_cost_usd),
          name: '费用',
          itemStyle: { color: '#1677ff' },
        },
        {
          type: 'line',
          data: stats.map((item) => item.request_count),
          name: '请求数',
          yAxisIndex: 1,
          smooth: true,
          itemStyle: { color: '#fa8c16' },
        },
      ],
    }),
    [stats],
  );

  const tokenPieOption = useMemo(
    () => ({
      tooltip: { trigger: 'item' },
      legend: { bottom: 0 },
      series: [
        {
          type: 'pie',
          radius: ['40%', '70%'],
          data: [
            { name: 'Input Tokens', value: summary.totalInputTokens },
            { name: 'Output Tokens', value: summary.totalOutputTokens },
          ],
          label: { formatter: '{b}: {d}%' },
        },
      ],
    }),
    [summary.totalInputTokens, summary.totalOutputTokens],
  );

  const columns = useMemo<TableProps<CostStats>['columns']>(
    () => [
      {
        title: '维度值',
        dataIndex: 'group_value',
        key: 'group_value',
        render: (value: string) => value || '-',
      },
      {
        title: 'Cost (USD)',
        dataIndex: 'total_cost_usd',
        key: 'total_cost_usd',
        defaultSortOrder: 'descend',
        sorter: (a, b) => a.total_cost_usd - b.total_cost_usd,
        render: (value: number) => `$${value.toFixed(6)}`,
      },
      {
        title: 'Requests',
        dataIndex: 'request_count',
        key: 'request_count',
        sorter: (a, b) => a.request_count - b.request_count,
      },
      {
        title: 'Input Tokens',
        dataIndex: 'total_input_tokens',
        key: 'total_input_tokens',
        sorter: (a, b) => a.total_input_tokens - b.total_input_tokens,
      },
      {
        title: 'Output Tokens',
        dataIndex: 'total_output_tokens',
        key: 'total_output_tokens',
        sorter: (a, b) => a.total_output_tokens - b.total_output_tokens,
      },
    ],
    [],
  );

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      {contextHolder}
      <Typography.Title level={2} style={{ margin: 0 }}>
        费用统计
      </Typography.Title>
      <Card>
        <Space wrap size={16}>
          <Space direction="vertical" size={4}>
            <Typography.Text type="secondary">时间范围</Typography.Text>
            <Select<RangePreset>
              value={preset}
              onChange={setPreset}
              options={[
                { label: '今天', value: 'today' },
                { label: '本周', value: 'week' },
                { label: '本月', value: 'month' },
                { label: '近30天', value: 'last30' },
                { label: '自定义', value: 'custom' },
              ]}
              style={{ width: 160 }}
            />
          </Space>
          {preset === 'custom' ? (
            <Space direction="vertical" size={4}>
              <Typography.Text type="secondary">自定义区间</Typography.Text>
              <DatePicker.RangePicker
                showTime
                value={customRange}
                onChange={(value) => {
                  setCustomRange(value as [Dayjs, Dayjs] | null);
                }}
              />
            </Space>
          ) : null}
          <Space direction="vertical" size={4}>
            <Typography.Text type="secondary">统计维度</Typography.Text>
            <Select<Dimension>
              value={dimension}
              onChange={setDimension}
              options={[
                { label: '按账号', value: 'account' },
                { label: '按密钥', value: 'key' },
                { label: '按模型', value: 'model' },
              ]}
              style={{ width: 160 }}
            />
          </Space>
        </Space>
      </Card>
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} lg={6}>
          <Card loading={loading}>
            <Statistic title="Total Cost" value={summary.totalCost} precision={6} prefix="$" />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card loading={loading}>
            <Statistic title="Total Requests" value={summary.totalRequests} />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card loading={loading}>
            <Statistic title="Total Input Tokens" value={summary.totalInputTokens} />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card loading={loading}>
            <Statistic title="Total Output Tokens" value={summary.totalOutputTokens} />
          </Card>
        </Col>
      </Row>
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={16}>
          <Card title={`费用趋势 / ${dimensionLabel}`}>
            {stats.length === 0 && !loading ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前区间没有统计数据" />
            ) : (
              <ReactECharts option={chartOption} style={{ height: 320 }} showLoading={loading} />
            )}
          </Card>
        </Col>
        <Col xs={24} xl={8}>
          <Card title="Token 分布">
            {summary.totalInputTokens === 0 && summary.totalOutputTokens === 0 && !loading ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前区间没有 Token 数据" />
            ) : (
              <ReactECharts option={tokenPieOption} style={{ height: 320 }} showLoading={loading} />
            )}
          </Card>
        </Col>
      </Row>
      <Card title="明细">
        <Table<CostStats>
          rowKey={(record) => `${record.group_by}-${record.group_value}`}
          loading={loading}
          columns={columns}
          dataSource={stats}
          pagination={{ pageSize: 20, showSizeChanger: false }}
        />
      </Card>
    </Space>
  );
};

export default Stats;
