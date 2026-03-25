import React, { useEffect, useMemo, useState } from 'react';
import type { AxiosError } from 'axios';
import dayjs, { type Dayjs } from 'dayjs';
import {
  Button,
  Card,
  DatePicker,
  Descriptions,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Spin,
  Table,
  Tabs,
  Tag,
  Typography,
  message,
} from 'antd';
import type { TableProps } from 'antd';
import { CheckCircleTwoTone, CloseCircleTwoTone, CopyOutlined } from '@ant-design/icons';
import { listAccounts } from '../api/accounts';
import { listKeys } from '../api/keys';
import { getPayload, queryLogs } from '../api/logs';
import type { Account, APIKey, RequestLog, RequestPayload } from '../types';

const PAGE_SIZE = 20;

interface LogFilterValues {
  range: [Dayjs, Dayjs];
  key_id?: string;
  account_id?: string;
  model?: string;
}

const getErrorMessage = (error: unknown) => {
  const axiosError = error as AxiosError<{ message?: string; error?: string }>;
  return axiosError.response?.data?.message ?? axiosError.response?.data?.error ?? axiosError.message ?? '请求失败';
};

const getTodayRange = (): [Dayjs, Dayjs] => [dayjs().startOf('day'), dayjs().endOf('day')];

const initialFilterValues = (): LogFilterValues => ({
  range: getTodayRange(),
  key_id: undefined,
  account_id: undefined,
  model: '',
});

/** Try to pretty-print JSON, return original on failure. */
const formatJson = (value: string): string => {
  if (!value) return '';
  try {
    return JSON.stringify(JSON.parse(value), null, 2);
  } catch {
    return value;
  }
};

/**
 * Parse SSE response body into formatted JSON blocks.
 * Each "data: {...}" line is extracted and pretty-printed.
 * Lines without JSON (event:, empty lines) are kept as-is.
 */
const formatSSE = (raw: string): string => {
  if (!raw) return '';
  const lines = raw.split('\n');
  const result: string[] = [];
  for (const line of lines) {
    if (line.startsWith('data: ')) {
      const jsonStr = line.substring(6).trim();
      if (jsonStr && jsonStr !== '[DONE]') {
        try {
          const obj = JSON.parse(jsonStr);
          result.push('data: ' + JSON.stringify(obj, null, 2));
          continue;
        } catch {
          // not valid JSON, keep as-is
        }
      }
    }
    result.push(line);
  }
  return result.join('\n');
};

const copyText = async (text: string, messageApi: ReturnType<typeof message.useMessage>[0]) => {
  await navigator.clipboard.writeText(text);
  messageApi.success('已复制');
};

const Logs: React.FC = () => {
  const [form] = Form.useForm<LogFilterValues>();
  const [messageApi, contextHolder] = message.useMessage();
  const [logs, setLogs] = useState<RequestLog[]>([]);
  const [keys, setKeys] = useState<APIKey[]>([]);
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(true);
  const [searching, setSearching] = useState(false);
  const [page, setPage] = useState(1);
  const [hasMore, setHasMore] = useState(false);
  const [total, setTotal] = useState(PAGE_SIZE);

  // Detail modal state
  const [detailLog, setDetailLog] = useState<RequestLog | null>(null);
  const [payloadLoading, setPayloadLoading] = useState(false);
  const [payload, setPayload] = useState<RequestPayload | null>(null);
  const [payloadExpired, setPayloadExpired] = useState(false);

  const loadOptions = async () => {
    try {
      const [keysResponse, accountsResponse] = await Promise.all([listKeys(), listAccounts()]);
      setKeys(keysResponse.data ?? []);
      setAccounts(accountsResponse.data ?? []);
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    }
  };

  const loadLogs = async (nextPage: number, values?: LogFilterValues) => {
    const filterValues = values ?? form.getFieldsValue();
    const [from, to] = filterValues.range ?? getTodayRange();
    setSearching(true);
    try {
      const response = await queryLogs({
        from: from.toDate().toISOString(),
        to: to.toDate().toISOString(),
        key_id: filterValues.key_id || undefined,
        account_id: filterValues.account_id || undefined,
        model: filterValues.model?.trim() || undefined,
        limit: PAGE_SIZE + 1,
        offset: (nextPage - 1) * PAGE_SIZE,
      });
      const data = response.data ?? [];
      const rows = data.slice(0, PAGE_SIZE);
      const nextHasMore = data.length > PAGE_SIZE;
      setLogs(rows);
      setPage(nextPage);
      setHasMore(nextHasMore);
      setTotal(nextHasMore ? nextPage * PAGE_SIZE + 1 : (nextPage - 1) * PAGE_SIZE + rows.length);
    } catch (error) {
      messageApi.error(getErrorMessage(error));
    } finally {
      setLoading(false);
      setSearching(false);
    }
  };

  useEffect(() => {
    form.setFieldsValue(initialFilterValues());
    void Promise.all([loadOptions(), loadLogs(1, initialFilterValues())]);
  }, []);

  const keyOptions = useMemo(
    () => (keys ?? []).map((key) => ({ label: `${key.id} (****${key.key_hint})`, value: key.id })),
    [keys],
  );

  const accountOptions = useMemo(
    () => (accounts ?? []).map((a) => ({ label: `${a.name || a.id} (${a.id})`, value: a.id })),
    [accounts],
  );

  const handleSearch = async () => { await loadLogs(1); };
  const handleReset = async () => {
    const values = initialFilterValues();
    form.resetFields();
    form.setFieldsValue(values);
    await loadLogs(1, values);
  };

  // Open detail modal and load payload
  const openDetail = async (record: RequestLog) => {
    setDetailLog(record);
    setPayload(null);
    setPayloadExpired(false);
    setPayloadLoading(true);
    try {
      const response = await getPayload(record.ID);
      setPayload(response.data);
    } catch (error) {
      const axiosError = error as AxiosError;
      if (axiosError.response?.status === 404) {
        setPayloadExpired(true);
      }
    } finally {
      setPayloadLoading(false);
    }
  };

  const columns = useMemo<TableProps<RequestLog>['columns']>(
    () => [
      {
        title: '时间',
        dataIndex: 'CreatedAt',
        key: 'CreatedAt',
        width: 180,
        render: (value: string) => new Date(value).toLocaleString(),
      },
      {
        title: 'Key',
        dataIndex: 'KeyHint',
        key: 'KeyHint',
        width: 100,
        render: (value: string) => value ? `****${value}` : '-',
      },
      {
        title: '账号',
        dataIndex: 'AccountID',
        key: 'AccountID',
        width: 140,
        ellipsis: true,
      },
      {
        title: '模型',
        key: 'model',
        width: 260,
        render: (_, record) =>
          record.ModelRequested === record.ModelActual ? (
            record.ModelRequested || '-'
          ) : (
            <Space size={4}>
              <Typography.Text>{record.ModelRequested || '-'}</Typography.Text>
              <Typography.Text type="secondary">&rarr;</Typography.Text>
              <Tag color="blue">{record.ModelActual || '-'}</Tag>
            </Space>
          ),
      },
      {
        title: 'Tokens',
        key: 'tokens',
        width: 140,
        render: (_, record) => `In:${record.InputTokens} Out:${record.OutputTokens}`,
      },
      {
        title: 'Cost',
        dataIndex: 'CostUSD',
        key: 'CostUSD',
        width: 100,
        render: (value: number) => `$${value.toFixed(6)}`,
      },
      {
        title: 'Latency',
        dataIndex: 'LatencyMs',
        key: 'LatencyMs',
        width: 90,
        render: (value: number) => `${value}ms`,
      },
      {
        title: '状态',
        key: 'status',
        width: 60,
        render: (_, record) =>
          record.StatusCode === 200 ? (
            <CheckCircleTwoTone twoToneColor="#52c41a" />
          ) : (
            <CloseCircleTwoTone twoToneColor="#ff4d4f" />
          ),
      },
    ],
    [],
  );

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      {contextHolder}
      <Typography.Title level={2} style={{ margin: 0 }}>请求日志</Typography.Title>
      <Card>
        <Form form={form} layout="vertical" onFinish={handleSearch}>
          <Space wrap align="end" size={16} style={{ width: '100%' }}>
            <Form.Item label="时间范围" name="range" style={{ minWidth: 320, marginBottom: 0 }}>
              <DatePicker.RangePicker allowClear={false} showTime style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item label="Key ID" name="key_id" style={{ minWidth: 220, marginBottom: 0 }}>
              <Select allowClear options={keyOptions} placeholder="全部密钥" showSearch optionFilterProp="label" />
            </Form.Item>
            <Form.Item label="Account ID" name="account_id" style={{ minWidth: 220, marginBottom: 0 }}>
              <Select allowClear options={accountOptions} placeholder="全部账号" showSearch optionFilterProp="label" />
            </Form.Item>
            <Form.Item label="Model" name="model" style={{ minWidth: 220, marginBottom: 0 }}>
              <Input placeholder="输入模型名" />
            </Form.Item>
            <Form.Item style={{ marginBottom: 0 }}>
              <Space>
                <Button type="primary" htmlType="submit" loading={searching}>搜索</Button>
                <Button onClick={() => void handleReset()}>重置</Button>
              </Space>
            </Form.Item>
          </Space>
        </Form>
      </Card>
      <Card>
        <Table<RequestLog>
          rowKey="ID"
          loading={loading}
          columns={columns}
          dataSource={logs}
          onRow={(record) => ({
            onClick: () => void openDetail(record),
            style: { cursor: 'pointer' },
          })}
          pagination={{
            current: page,
            pageSize: PAGE_SIZE,
            total,
            showSizeChanger: false,
            onChange: (nextPage) => { void loadLogs(nextPage); },
          }}
        />
        {!hasMore && logs.length === 0 && !loading ? (
          <Typography.Text type="secondary">当前筛选条件下没有日志。</Typography.Text>
        ) : null}
      </Card>

      {/* Detail Modal */}
      <Modal
        title={detailLog ? `请求详情 #${detailLog.ID}` : '请求详情'}
        open={!!detailLog}
        onCancel={() => setDetailLog(null)}
        footer={null}
        width={900}
        styles={{ body: { maxHeight: '70vh', overflow: 'auto' } }}
      >
        {detailLog && (
          <Space direction="vertical" size={16} style={{ width: '100%' }}>
            {/* Metrics */}
            <Descriptions bordered size="small" column={2}>
              <Descriptions.Item label="时间">{new Date(detailLog.CreatedAt).toLocaleString()}</Descriptions.Item>
              <Descriptions.Item label="状态">
                {detailLog.StatusCode === 200 ? (
                  <Tag color="green">成功</Tag>
                ) : (
                  <Tag color="red">{detailLog.StatusCode} {detailLog.Error}</Tag>
                )}
              </Descriptions.Item>
              <Descriptions.Item label="Key">{detailLog.KeyID} (****{detailLog.KeyHint})</Descriptions.Item>
              <Descriptions.Item label="账号">{detailLog.AccountName || detailLog.AccountID}</Descriptions.Item>
              <Descriptions.Item label="Provider">{detailLog.Provider}</Descriptions.Item>
              <Descriptions.Item label="Stop Reason">{detailLog.StopReason || '-'}</Descriptions.Item>
              <Descriptions.Item label="请求模型">{detailLog.ModelRequested}</Descriptions.Item>
              <Descriptions.Item label="实际模型">
                {detailLog.ModelRequested !== detailLog.ModelActual ? (
                  <Tag color="blue">{detailLog.ModelActual}</Tag>
                ) : detailLog.ModelActual}
              </Descriptions.Item>
              <Descriptions.Item label="Input Tokens">{detailLog.InputTokens.toLocaleString()}</Descriptions.Item>
              <Descriptions.Item label="Output Tokens">{detailLog.OutputTokens.toLocaleString()}</Descriptions.Item>
              <Descriptions.Item label="费用">${detailLog.CostUSD.toFixed(6)}</Descriptions.Item>
              <Descriptions.Item label="延迟">{detailLog.LatencyMs}ms</Descriptions.Item>
            </Descriptions>

            {/* Payload */}
            {payloadLoading ? (
              <div style={{ textAlign: 'center', padding: 24 }}><Spin /></div>
            ) : payloadExpired ? (
              <Typography.Text type="secondary">Payload 已过期（超过保留期）</Typography.Text>
            ) : payload ? (
              <Tabs
                items={[
                  {
                    key: 'request',
                    label: 'Request Body',
                    children: (
                      <div style={{ position: 'relative' }}>
                        <Button
                          icon={<CopyOutlined />}
                          size="small"
                          style={{ position: 'absolute', top: 8, right: 8, zIndex: 1 }}
                          onClick={() => void copyText(payload.RequestBody, messageApi)}
                        >
                          复制
                        </Button>
                        <pre style={{
                          margin: 0,
                          padding: 16,
                          background: '#f5f5f5',
                          borderRadius: 6,
                          overflow: 'auto',
                          maxHeight: 400,
                          fontSize: 12,
                          lineHeight: 1.6,
                        }}>
                          {formatJson(payload.RequestBody)}
                        </pre>
                      </div>
                    ),
                  },
                  {
                    key: 'response',
                    label: 'Response Body',
                    children: (
                      <div style={{ position: 'relative' }}>
                        <Button
                          icon={<CopyOutlined />}
                          size="small"
                          style={{ position: 'absolute', top: 8, right: 8, zIndex: 1 }}
                          onClick={() => void copyText(payload.ResponseBody, messageApi)}
                        >
                          复制
                        </Button>
                        <pre style={{
                          margin: 0,
                          padding: 16,
                          background: '#f5f5f5',
                          borderRadius: 6,
                          overflow: 'auto',
                          maxHeight: 400,
                          fontSize: 12,
                          lineHeight: 1.6,
                        }}>
                          {formatSSE(payload.ResponseBody)}
                        </pre>
                      </div>
                    ),
                  },
                ]}
              />
            ) : null}
          </Space>
        )}
      </Modal>
    </Space>
  );
};

export default Logs;
